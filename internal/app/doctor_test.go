package app

import (
	"errors"
	"strings"
	"testing"
)

func TestImportHintTellsHowToStartClean(t *testing.T) {
	exists := importHint(errors.New("ERROR 1050 (42S01) at line 25: Table 'users' already exists"))
	other := importHint(errors.New("ERROR 1452 (23000) at line 207902: Cannot add or update a child row"))
	for name, h := range map[string]string{"already exists": exists, "other failure": other} {
		if !strings.Contains(h, "--fresh") || !strings.Contains(h, "backup") {
			t.Errorf("%s: the hint must name --fresh and say a backup is kept: %q", name, h)
		}
	}
	if !strings.Contains(exists, "already has these tables") {
		t.Errorf("the 'already exists' case should say why: %q", exists)
	}
}
