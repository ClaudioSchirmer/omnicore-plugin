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
