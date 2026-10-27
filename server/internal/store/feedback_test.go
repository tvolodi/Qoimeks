package store_test

import (
	"encoding/json"
	"testing"

	"qoimeks/server/internal/store"
	"qoimeks/server/internal/store/storetest"
)

func TestFeedbackAppendAndList(t *testing.T) {
	st := storetest.New(t)
	ctx := t.Context()
	voice := "голосом"
	a, err := st.AppendFeedback(ctx, store.Feedback{Kind: "user", Text: "one", VoiceText: &voice, Errors: json.RawMessage(`["e1"]`)})
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.AppendFeedback(ctx, store.Feedback{Kind: "user", Text: "two"})
	if err != nil {
		t.Fatal(err)
	}
	if b <= a {
		t.Fatalf("ids not increasing: %d, %d", a, b)
	}
	all, err := st.ListFeedback(ctx, 0, 10)
	if err != nil || len(all) != 2 {
		t.Fatalf("list: %v %+v", err, all)
	}
	if all[0].VoiceText == nil || *all[0].VoiceText != voice || string(all[0].Errors) != `["e1"]` || all[1].VoiceText != nil || string(all[1].Errors) != `[]` {
		t.Fatalf("bad rows: %+v", all)
	}
	rest, _ := st.ListFeedback(ctx, a, 10)
	if len(rest) != 1 || rest[0].ID != b {
		t.Fatalf("after %d: %+v", a, rest)
	}
	if one, _ := st.ListFeedback(ctx, 0, 1); len(one) != 1 {
		t.Fatalf("limit: %+v", one)
	}
	if _, err := st.AppendFeedback(ctx, store.Feedback{Text: "no kind"}); err == nil {
		t.Fatal("empty kind accepted")
	}
}

func TestFeedbackIsAppendOnly(t *testing.T) {
	st := storetest.New(t)
	ctx := t.Context()
	if _, err := st.AppendFeedback(ctx, store.Feedback{Kind: "user", Text: "x"}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`update feedback set text = 'y'`,
		`delete from feedback`,
		`truncate feedback`,
	} {
		if _, err := st.Pool().Exec(ctx, q); err == nil {
			t.Errorf("%q succeeded", q)
		}
	}
	if rows, _ := st.ListFeedback(ctx, 0, 10); len(rows) != 1 || rows[0].Text != "x" {
		t.Fatalf("row changed: %+v", rows)
	}
}
