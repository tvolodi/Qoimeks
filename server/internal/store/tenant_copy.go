package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// copyTables lists the ledger tables in the order they can be filled without breaking
// a foreign key, with the column rows are ordered by in checksums. movements.reverses
// points at the same table and movements.import_id is deferred, so one statement per
// table is enough.
var copyTables = []struct{ name, orderBy string }{
	{"locations", "id"},
	{"skus", "id"},
	{"imports", "id"},
	{"movements", "seq"},
	{"balances", "id"},
	{"feedback", "id"},
}

// ErrCopyMismatch: the target does not equal the source after copying.
var ErrCopyMismatch = errors.New("copy does not match the source")

// ErrTargetNotEmpty: the tenant schema already holds ledger rows.
var ErrTargetNotEmpty = errors.New("tenant schema is not empty")

// CopyOptions selects what CopyToTenant copies.
type CopyOptions struct {
	TenantID     uuid.UUID
	SourceSchema string // default "public"
	DryRun       bool   // do everything, then roll back
}

// TableCopy is the report line of one table. Checksums are md5 of the ordered rows;
// the rows themselves never leave the database.
type TableCopy struct {
	Name         string
	SourceRows   int64
	TargetRows   int64
	SourceSum    string
	TargetSum    string
	Elapsed      time.Duration
	SequenceName string // empty when the table has no identity column
}

// TableAnalyze is the result of the ANALYZE run on a copied table after the commit.
type TableAnalyze struct {
	Name    string
	Elapsed time.Duration
	Err     error // the copy stays committed; the operator runs ANALYZE by hand
}

// CopyReport is what CopyToTenant returns, also with an error when verification failed.
type CopyReport struct {
	TenantID      uuid.UUID
	SourceSchema  string
	TargetSchema  string
	DryRun        bool
	Tables        []TableCopy
	Analyzed      []TableAnalyze // empty on a dry run or when the copy failed
	SourceCurrent string         // checksum of the current balance per SKU and location
	TargetCurrent string
	Elapsed       time.Duration
}

func ident(parts ...string) string { return pgx.Identifier(parts).Sanitize() }

// CopyToTenant copies, never moves, the ledger tables of the source schema into the
// existing schema of a tenant, in one transaction on a consistent snapshot: ids,
// timestamps and identity sequence positions are kept. It refuses when the tenant is
// unknown, when a target table has rows, or when the table shapes differ. Before the
// commit it compares row counts and checksums per table and the current balances;
// any difference aborts with ErrCopyMismatch and nothing is changed. With DryRun the
// transaction is always rolled back. After a real commit each copied table is analyzed;
// a failure there is recorded in the report, not returned.
func (s *Store) CopyToTenant(ctx context.Context, opt CopyOptions) (CopyReport, error) {
	start := time.Now()
	src := opt.SourceSchema
	if src == "" {
		src = "public"
	}
	rep := CopyReport{TenantID: opt.TenantID, SourceSchema: src, DryRun: opt.DryRun}

	tn, err := s.Tenant(ctx, opt.TenantID)
	if errors.Is(err, ErrNotFound) {
		return rep, fmt.Errorf("tenant %s does not exist", opt.TenantID)
	}
	if err != nil {
		return rep, err
	}
	dst := tn.SchemaName
	rep.TargetSchema = dst
	if src == dst {
		return rep, invalid("source and target schema are the same")
	}

	// Same per-tenant lock as CreateTenant, held on a session so it is taken before the
	// transaction's snapshot: a second copy waits and then sees the filled target.
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return rep, err
	}
	defer conn.Release()
	lockKey := "qoimeks-tenant:" + opt.TenantID.String()
	if _, err := conn.Exec(ctx, `select pg_advisory_lock(hashtext($1))`, lockKey); err != nil {
		return rep, err
	}
	defer func() {
		if _, err := conn.Exec(context.Background(), `select pg_advisory_unlock(hashtext($1))`, lockKey); err != nil {
			conn.Conn().Close(context.Background())
		}
	}()

	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return rep, err
	}
	committed := false
	// Sequences are not transactional: remember where the target's were, to put them back.
	type seqState struct {
		name   string
		last   int64
		called bool
	}
	var restore []seqState
	defer func() {
		if committed {
			return
		}
		tx.Rollback(context.Background())
		for _, q := range restore {
			conn.Exec(context.Background(), `select setval($1::regclass, $2, $3)`, q.name, q.last, q.called)
		}
	}()

	var srcExists bool
	if err := tx.QueryRow(ctx, `select exists(select 1 from pg_namespace where nspname = $1)`, src).Scan(&srcExists); err != nil {
		return rep, err
	}
	if !srcExists {
		return rep, fmt.Errorf("source schema %q does not exist", src)
	}

	type plan struct {
		cols     []string
		identity []string
	}
	plans := make([]plan, len(copyTables))
	for i, t := range copyTables {
		sc, si, err := tableShape(ctx, tx, src, t.name)
		if err != nil {
			return rep, err
		}
		dc, _, err := tableShape(ctx, tx, dst, t.name)
		if err != nil {
			return rep, err
		}
		if strings.Join(sc, ",") != strings.Join(dc, ",") {
			return rep, fmt.Errorf("table %s differs between %s and %s (columns); migrate the tenant schema first", t.name, src, dst)
		}
		var n int64
		if err := tx.QueryRow(ctx, `select count(*) from `+ident(dst, t.name)).Scan(&n); err != nil {
			return rep, err
		}
		if n > 0 {
			return rep, fmt.Errorf("%w: %s.%s has %d rows", ErrTargetNotEmpty, dst, t.name, n)
		}
		plans[i] = plan{cols: sc, identity: si}
	}

	var mismatches []string
	for i, t := range copyTables {
		began := time.Now()
		cols := make([]string, len(plans[i].cols))
		for j, c := range plans[i].cols {
			cols[j] = ident(c)
		}
		list := strings.Join(cols, ", ")
		over := ""
		if len(plans[i].identity) > 0 {
			over = " overriding system value"
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf(`insert into %s (%s)%s select %s from %s order by %s`,
			ident(dst, t.name), list, over, list, ident(src, t.name), t.orderBy)); err != nil {
			return rep, fmt.Errorf("copy %s: %w", t.name, err)
		}
		tc := TableCopy{Name: t.name}
		if tc.SourceRows, tc.SourceSum, err = tableSum(ctx, tx, src, t.name, plans[i].cols, t.orderBy); err != nil {
			return rep, err
		}
		if tc.TargetRows, tc.TargetSum, err = tableSum(ctx, tx, dst, t.name, plans[i].cols, t.orderBy); err != nil {
			return rep, err
		}
		if tc.SourceRows != tc.TargetRows || tc.SourceSum != tc.TargetSum {
			mismatches = append(mismatches, t.name)
		}
		tc.Elapsed = time.Since(began)
		rep.Tables = append(rep.Tables, tc)
	}
	if rep.SourceCurrent, err = currentSum(ctx, tx, src); err != nil {
		return rep, err
	}
	if rep.TargetCurrent, err = currentSum(ctx, tx, dst); err != nil {
		return rep, err
	}
	if rep.SourceCurrent != rep.TargetCurrent {
		mismatches = append(mismatches, "current balances")
	}
	if len(mismatches) > 0 {
		return rep, fmt.Errorf("%w: %s", ErrCopyMismatch, strings.Join(mismatches, ", "))
	}

	// Sequences last: a dry run leaves them alone, since setval cannot be rolled back.
	for i, t := range copyTables {
		for _, col := range plans[i].identity {
			var srcSeq, dstSeq string
			if err := tx.QueryRow(ctx, `select pg_get_serial_sequence($1, $2)`, ident(src, t.name), col).Scan(&srcSeq); err != nil {
				return rep, err
			}
			if err := tx.QueryRow(ctx, `select pg_get_serial_sequence($1, $2)`, ident(dst, t.name), col).Scan(&dstSeq); err != nil {
				return rep, err
			}
			var last int64
			var called bool
			if err := tx.QueryRow(ctx, `select last_value, is_called from `+srcSeq).Scan(&last, &called); err != nil {
				return rep, err
			}
			rep.Tables[i].SequenceName = col
			if opt.DryRun {
				continue
			}
			var q seqState
			q.name = dstSeq
			if err := tx.QueryRow(ctx, `select last_value, is_called from `+dstSeq).Scan(&q.last, &q.called); err != nil {
				return rep, err
			}
			restore = append(restore, q)
			if _, err := tx.Exec(ctx, `select setval($1::regclass, $2, $3)`, dstSeq, last, called); err != nil {
				return rep, fmt.Errorf("set sequence of %s: %w", t.name, err)
			}
		}
	}

	if opt.DryRun {
		rep.Elapsed = time.Since(start)
		return rep, nil // the deferred rollback undoes the copy
	}
	if err := tx.Commit(ctx); err != nil {
		return rep, err
	}
	committed = true
	// The copy is committed; ANALYZE failing must not turn it into a failure. Without
	// statistics the first reads of a large tenant take minutes.
	for _, t := range copyTables {
		began := time.Now()
		ta := TableAnalyze{Name: t.name}
		if _, err := conn.Exec(ctx, `analyze `+ident(dst, t.name)); err != nil {
			ta.Err = err
		}
		ta.Elapsed = time.Since(began)
		rep.Analyzed = append(rep.Analyzed, ta)
	}
	rep.Elapsed = time.Since(start)
	return rep, nil
}

// tableShape returns the column names in order and the identity columns of a table.
func tableShape(ctx context.Context, tx pgx.Tx, schema, table string) (cols, identity []string, err error) {
	rows, err := tx.Query(ctx, `
		select column_name, is_identity = 'YES' from information_schema.columns
		where table_schema = $1 and table_name = $2 order by ordinal_position`, schema, table)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var id bool
		if err := rows.Scan(&name, &id); err != nil {
			return nil, nil, err
		}
		cols = append(cols, name)
		if id {
			identity = append(identity, name)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if len(cols) == 0 {
		return nil, nil, fmt.Errorf("table %s.%s does not exist", schema, table)
	}
	return cols, identity, nil
}

// tableSum returns the row count and an md5 over the rows in order, all computed in the database.
func tableSum(ctx context.Context, tx pgx.Tx, schema, table string, cols []string, orderBy string) (int64, string, error) {
	q := make([]string, len(cols))
	for i, c := range cols {
		q[i] = ident(c)
	}
	var n int64
	var sum string
	err := tx.QueryRow(ctx, fmt.Sprintf(
		`select count(*), coalesce(md5(string_agg(row(%s)::text, E'\n' order by %s)), '') from %s`,
		strings.Join(q, ", "), ident(orderBy), ident(schema, table))).Scan(&n, &sum)
	return n, sum, err
}

// currentSum is a checksum of the current balance (latest row) per SKU and location.
func currentSum(ctx context.Context, tx pgx.Tx, schema string) (string, error) {
	var sum string
	err := tx.QueryRow(ctx, fmt.Sprintf(`
		select coalesce(md5(string_agg(sku_id || ':' || location_id || ':' || qty, ',' order by sku_id, location_id)), '')
		from (select distinct on (sku_id, location_id) sku_id, location_id, qty
		      from %s order by sku_id, location_id, id desc) c`, ident(schema, "balances"))).Scan(&sum)
	return sum, err
}
