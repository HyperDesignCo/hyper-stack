package project

import (
	"errors"
	"strings"
	"testing"
)

const valid = `{"name":"bazooka","php":"8.3","database":{"engine":"mysql","name":"bazooka_local"}}`

func TestParseValidAppliesDefaults(t *testing.T) {
	c, err := Parse([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	if c.Domain != "bazooka.test" || c.Node != "20" {
		t.Fatalf("defaults not applied: %+v", c)
	}
}

func TestParseReportsEveryBadField(t *testing.T) {
	_, err := Parse([]byte(`{"name":"Bad Name","domain":"x.com","php":"7.4","database":{"engine":"pg","name":"A-B"},"services":["kafka"]}`))
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want ValidationError, got %v", err)
	}
	for _, f := range []string{"name:", "domain:", "php:", "database.engine:", "database.name:", "services[0]:"} {
		if !strings.Contains(err.Error(), f) {
			t.Errorf("missing problem for %s in %v", f, err)
		}
	}
}

func TestParseRejectsUnknownField(t *testing.T) {
	if _, err := Parse([]byte(`{"nane":"x"}`)); err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestMissingRequired(t *testing.T) {
	_, err := Parse([]byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "name: is required") {
		t.Fatalf("got %v", err)
	}
}
