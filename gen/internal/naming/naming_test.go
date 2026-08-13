package naming

import "testing"

func TestSnake(t *testing.T) {
	for in, want := range map[string]string{
		"Name":             "name",
		"EnrollmentNumber": "enrollment_number",
		"ID":               "id",
		"HTTPServer":       "http_server",
		"UserID":           "user_id",
	} {
		if got := Snake(in); got != want {
			t.Errorf("Snake(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCamel(t *testing.T) {
	for in, want := range map[string]string{
		"Name":             "name",
		"EnrollmentNumber": "enrollmentNumber",
		"ID":               "id",
		"IDNumber":         "idNumber",
	} {
		if got := Camel(in); got != want {
			t.Errorf("Camel(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSingularPreservesUsIs pins the specific mistake a naive "strip the s"
// rule makes: it turns "status" into "statu" and silently mis-names a route.
func TestSingularPreservesUsIs(t *testing.T) {
	for in, want := range map[string]string{
		"status":   "status",
		"analysis": "analysis",
		"students": "student",
		"grades":   "grade",
		"policies": "policy",
		"boxes":    "box",
		"address":  "address",
	} {
		if got := Singular(in); got != want {
			t.Errorf("Singular(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPlural(t *testing.T) {
	for in, want := range map[string]string{
		"student": "students",
		"box":     "boxes",
		"policy":  "policies",
		"day":     "days",
		"address": "addresses",
	} {
		if got := Plural(in); got != want {
			t.Errorf("Plural(%q) = %q, want %q", in, got, want)
		}
	}
}
