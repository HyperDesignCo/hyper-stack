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
