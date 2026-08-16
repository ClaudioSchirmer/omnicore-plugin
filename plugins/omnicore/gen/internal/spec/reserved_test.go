package spec

import "testing"

// TestReservedWordsAreRefused pins the failure this guards against: a column
// named for a reserved word applies on the engine the developer uses and is
// rejected on the one they do not.
func TestReservedWordsAreRefused(t *testing.T) {
	for _, name := range []string{"order", "number", "level", "user", "limit", "identity", "rank"} {
		if ReservedWord(name) == "" {
			t.Errorf("%q should be recognised as reserved on some engine", name)
		}
	}
	for _, name := range []string{"enrollment_number", "full_name", "grade", "student_id"} {
		if engine := ReservedWord(name); engine != "" {
			t.Errorf("%q was wrongly flagged as reserved on %s", name, engine)
		}
	}
}

func TestReservedWordIsCaseInsensitive(t *testing.T) {
	if ReservedWord("ORDER") == "" {
		t.Error("the check must not depend on how the name is cased")
	}
}

func TestReservedColumnBlocksValidation(t *testing.T) {
	s := minimalSpec()
	s.Fields[0].Column = "order"
	ps := Validate(s, Options{})
	if !ps.HasBlockers() {
		t.Fatal("a reserved column name must be refused")
	}
}
