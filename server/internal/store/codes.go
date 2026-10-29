package store

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	ObjectSKU      = "sku"
	ObjectLocation = "location"
)

// ErrCodeInUse: the code is active on another object. The error is a *CodeInUseError.
var ErrCodeInUse = errors.New("code already in use")

// CodeInUseError carries the owner of the code that was asked for.
type CodeInUseError struct {
	Code  string
	Owner CodeOwner
}

// CodeOwner is the object a code points at.
type CodeOwner struct {
	ObjectType string `json:"object_type"`
	ObjectID   int64  `json:"object_id"`
	ObjectCode string `json:"object_code"`
	ObjectName string `json:"object_name"`
}

func (e *CodeInUseError) Error() string {
	return fmt.Sprintf("code %s is already used by %s %s", e.Code, e.Owner.ObjectCode, e.Owner.ObjectName)
}

func (e *CodeInUseError) Is(target error) bool { return target == ErrCodeInUse }

// Code is one row of the codes table with the object's own code and name.
type Code struct {
	ID         uuid.UUID  `json:"id"`
	ObjectType string     `json:"object_type"`
	ObjectID   int64      `json:"object_id"`
	ObjectCode string     `json:"object_code"`
	ObjectName string     `json:"object_name"`
	CodeType   string     `json:"code_type"`
	Code       string     `json:"code"`
	CreatedBy  string     `json:"created_by"`
	CreatedAt  time.Time  `json:"created_at"`
	RemovedAt  *time.Time `json:"removed_at,omitempty"`
	RemovedBy  string     `json:"removed_by,omitempty"`
}

// NewCode is the request to assign a code to an object.
type NewCode struct {
	ID         uuid.UUID
	ObjectType string
	ObjectID   int64
	CodeType   string
	Code       string
	Operator   string
}

// EANCheckDigitValid reports whether s is 13 (EAN-13) or 8 (EAN-8) digits with a valid check digit.
func EANCheckDigitValid(s string) bool {
	if len(s) != 13 && len(s) != 8 {
		return false
	}
	sum := 0
	for i := 0; i < len(s)-1; i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
		w := 1
		// Weight 3 goes on the digits next to the check digit, then alternates.
		if (len(s)-1-i)%2 == 1 {
			w = 3
		}
		sum += int(s[i]-'0') * w
	}
	last := s[len(s)-1]
	return last >= '0' && last <= '9' && (10-sum%10)%10 == int(last-'0')
}

func validateCode(in NewCode) error {
	if in.ID == uuid.Nil {
		return invalid("id is required")
	}
	switch in.ObjectType {
	case ObjectSKU, ObjectLocation:
	default:
		return invalid("unknown object type %q", in.ObjectType)
	}
	if in.Operator == "" {
		return invalid("operator is required")
	}
	n := utf8.RuneCountInString(in.Code)
	if n == 0 {
		return invalid("code is empty")
	}
	for _, r := range in.Code {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return invalid("code must not contain spaces or control characters")
		}
	}
	switch in.CodeType {
	case "ean13":
		if len(in.Code) != 13 || !EANCheckDigitValid(in.Code) {
			return invalid("%q is not a valid EAN-13", in.Code)
		}
	case "ean8":
		if len(in.Code) != 8 || !EANCheckDigitValid(in.Code) {
			return invalid("%q is not a valid EAN-8", in.Code)
		}
	case "code128":
		if n > 48 {
			return invalid("code128 is at most 48 characters")
		}
		for _, r := range in.Code {
			if r < 0x20 || r > 0x7e {
				return invalid("code128 takes printable ASCII only")
			}
		}
	case "qr":
		if n > 256 {
			return invalid("qr code is at most 256 characters")
		}
	case "manual":
		if n > 64 {
			return invalid("manual code is at most 64 characters")
		}
	default:
		return invalid("unknown code type %q", in.CodeType)
	}
	return nil
}

const codeSelect = `
	select c.id, c.object_type, c.object_id,
	       coalesce(s.code, l.code, ''), coalesce(s.name, l.name, ''),
	       c.code_type, c.code, c.created_by, c.created_at, c.removed_at, coalesce(c.removed_by, '')
	from codes c
	left join skus s on c.object_type = 'sku' and s.id = c.object_id
	left join locations l on c.object_type = 'location' and l.id = c.object_id `

func scanCode(row pgx.Row) (Code, error) {
	var c Code
	err := row.Scan(&c.ID, &c.ObjectType, &c.ObjectID, &c.ObjectCode, &c.ObjectName,
		&c.CodeType, &c.Code, &c.CreatedBy, &c.CreatedAt, &c.RemovedAt, &c.RemovedBy)
	return c, err
}

func sameCode(c Code, in NewCode) bool {
	return c.ObjectType == in.ObjectType && c.ObjectID == in.ObjectID && c.CodeType == in.CodeType && c.Code == in.Code
}

// AssignCode gives a code to a SKU or location in one transaction. The second result is true
// when a row was inserted; false when the same id (same content) or an active identical
// assignment already existed and that row is returned. A code active on another object is a
// *CodeInUseError; the same id with different content is ErrConflict.
func (s *Store) AssignCode(ctx context.Context, in NewCode) (Code, bool, error) {
	if err := validateCode(in); err != nil {
		return Code{}, false, err
	}
	var out Code
	created := false
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var ok bool
		q := `select exists(select 1 from skus where id = $1 and active)`
		if in.ObjectType == ObjectLocation {
			q = `select exists(select 1 from locations where id = $1 and active)`
		}
		if err := tx.QueryRow(ctx, q, in.ObjectID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: %s %d", ErrNotFound, in.ObjectType, in.ObjectID)
		}
		// Serialize assignments of one code, so the read-then-insert below cannot race.
		if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('codes:' || $1))`, in.Code); err != nil {
			return err
		}
		if c, err := scanCode(tx.QueryRow(ctx, codeSelect+`where c.id = $1`, in.ID)); err == nil {
			if !sameCode(c, in) {
				return ErrConflict
			}
			out = c
			return nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		_, err := tx.Exec(ctx, `insert into codes (id, object_type, object_id, code_type, code, created_by)
			values ($1, $2, $3, $4, $5, $6)`, in.ID, in.ObjectType, in.ObjectID, in.CodeType, in.Code, in.Operator)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.ConstraintName == "codes_code_active_uq" {
				return errCodeTaken
			}
			return err
		}
		created = true
		var e error
		out, e = scanCode(tx.QueryRow(ctx, codeSelect+`where c.id = $1`, in.ID))
		return e
	})
	if errors.Is(err, errCodeTaken) {
		// The failed insert aborted the transaction; read the owner in a fresh one.
		active, lerr := s.LookupCode(ctx, in.Code)
		if lerr != nil {
			return Code{}, false, fmt.Errorf("find owner of code: %w", lerr)
		}
		if active.ObjectType == in.ObjectType && active.ObjectID == in.ObjectID {
			return active, false, nil
		}
		return Code{}, false, &CodeInUseError{Code: in.Code, Owner: CodeOwner{
			ObjectType: active.ObjectType, ObjectID: active.ObjectID,
			ObjectCode: active.ObjectCode, ObjectName: active.ObjectName}}
	}
	if err != nil {
		return Code{}, false, err
	}
	return out, created, nil
}

var errCodeTaken = errors.New("code taken")

// ListCodes returns the active codes of an object, oldest first.
func (s *Store) ListCodes(ctx context.Context, objectType string, objectID int64) ([]Code, error) {
	if objectType != ObjectSKU && objectType != ObjectLocation {
		return nil, invalid("unknown object type %q", objectType)
	}
	rows, err := s.pool.Query(ctx, codeSelect+`where c.object_type = $1 and c.object_id = $2 and c.removed_at is null
		order by c.created_at, c.id`, objectType, objectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Code{}
	for rows.Next() {
		c, err := scanCode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// LookupCode finds the object an active code points at. The match is case-sensitive.
func (s *Store) LookupCode(ctx context.Context, code string) (Code, error) {
	c, err := scanCode(s.pool.QueryRow(ctx, codeSelect+`where c.code = $1 and c.removed_at is null`, code))
	if errors.Is(err, pgx.ErrNoRows) {
		return Code{}, ErrNotFound
	}
	return c, err
}

// RemoveCode soft-removes a code by id, keeping the row as history and freeing the code.
// Removing an already removed code returns the row unchanged.
func (s *Store) RemoveCode(ctx context.Context, id uuid.UUID, operator string) (Code, error) {
	if operator == "" {
		return Code{}, invalid("operator is required")
	}
	var out Code
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `update codes set removed_at = now(), removed_by = $2
			where id = $1 and removed_at is null`, id, operator); err != nil {
			return err
		}
		c, err := scanCode(tx.QueryRow(ctx, codeSelect+`where c.id = $1`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		out = c
		return err
	})
	return out, err
}
