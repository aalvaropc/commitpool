package ai

import (
	"fmt"
	"testing"
)

func TestCompiledSchema_CachedPerDateSet(t *testing.T) {
	first, err := compiledSchema([]string{"sun", "sat"})
	if err != nil {
		t.Fatalf("compiledSchema: %v", err)
	}
	second, err := compiledSchema([]string{"sat", "sun"})
	if err != nil {
		t.Fatalf("compiledSchema: %v", err)
	}
	if first != second {
		t.Error("the same set of dates compiled twice")
	}
	other, err := compiledSchema([]string{"sat"})
	if err != nil || other == first {
		t.Errorf("another set of dates = %p, %v; want its own schema", other, err)
	}
	for i := range schemaCacheSize + 10 {
		if _, err := compiledSchema([]string{fmt.Sprint("d", i)}); err != nil {
			t.Fatalf("compiledSchema: %v", err)
		}
	}
	schemas.mu.Lock()
	n := len(schemas.byDates)
	schemas.mu.Unlock()
	if n > schemaCacheSize {
		t.Errorf("schema cache holds %d entries, want at most %d", n, schemaCacheSize)
	}
}
