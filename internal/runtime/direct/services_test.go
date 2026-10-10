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
