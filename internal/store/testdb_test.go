package store

import "testing"

func TestTestDatabaseName_RequiresTestSuffix(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"postgres://u:p@h:5432/commitpool_test?sslmode=disable", true},
		{"postgres://u:p@h:5432/commitpool_test", true},
		{"postgres://u:p@h:5432/commitpool", false},
		{"postgres://u:p@h:5432/commitpool_sandbox?sslmode=disable", false},
		{"postgres://u:p@h:5432/", false},
		{"::not a url", false},
	}
	for _, c := range cases {
		err := checkTestDatabaseName(c.url)
		if ok := err == nil; ok != c.want {
			t.Errorf("checkTestDatabaseName(%q) = %v, want ok=%v", c.url, err, c.want)
		}
	}
}
