package direct

import (
	"path/filepath"
	"testing"
)

func TestOffendingConf(t *testing.T) {
	sites := filepath.FromSlash("E:/HyperLab/stack/run/nginx/sites")
	cases := []struct{ name, out, want string }{
		{"own site file",
			`nginx: [emerg] invalid number of arguments in "root" directive in E:/HyperLab/stack/run/nginx/sites/my-cool-site.conf:12` + "\n" + `nginx: configuration file E:\HyperLab\stack\run\nginx\nginx.conf test failed`,
			filepath.FromSlash("E:/HyperLab/stack/run/nginx/sites/my-cool-site.conf")},
		{"the main config is not ours to set aside",
			`nginx: [emerg] unknown directive "x" in E:/HyperLab/stack/run/nginx/nginx.conf:9`, ""},
		{"a file outside the sites folder is ignored",
			`nginx: [emerg] bad in C:/elsewhere/other.conf:3`, ""},
		{"no file in the message", `nginx: [emerg] could not open error log file`, ""},
	}
	for _, c := range cases {
		if got := offendingConf(c.out, sites); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestImportTurnsForeignKeyChecksOff(t *testing.T) {
	args := importArgs(`C:\x\my.cnf`, "medical")
	has := func(want string) bool {
		for _, a := range args {
			if a == want {
				return true
			}
		}
		return false
	}
	if !has("--init-command=SET FOREIGN_KEY_CHECKS=0") {
		t.Errorf("a dump must load with foreign key checks off, as mysqldump restores: %v", args)
	}
	if args[len(args)-1] != "medical" || !has("-uroot") {
		t.Errorf("database must be the last argument: %v", args)
	}
}

// Every PHP version needs its own worker ports. 7.4 and 8.4 both end in "4",
// which used to be the only thing that decided the port.
func TestPHPVersionsNeverShareWorkerPorts(t *testing.T) {
	r := &Runtime{}
	r.S.FPMBasePort = 9000
	seen := map[int]string{}
	for _, v := range []string{"7.4", "8.0", "8.1", "8.2", "8.3", "8.4"} {
		for i := 0; i < FPMWorkers; i++ {
			p := r.FPMPort(v, i)
			if other, clash := seen[p]; clash {
				t.Fatalf("PHP %s worker %d and PHP %s share port %d", v, i, other, p)
			}
			seen[p] = v
		}
	}
	// installs made before 7.x existed keep exactly the ports they had
	for v, want := range map[string]int{"8.2": 9020, "8.3": 9030, "8.4": 9040} {
		if got := r.FPMPort(v, 0); got != want {
			t.Errorf("PHP %s moved from port %d to %d", v, want, got)
		}
	}
}
