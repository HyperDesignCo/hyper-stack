package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/hyperdesign/hyper-stack/internal/config"
)

// askInstallDir asks where to install, the way Laragon and XAMPP do. Without a
// terminal (scripts, CI) it uses the default folder and says so.
func askInstallDir() (string, error) {
	def := config.DefaultInstallDir()
	fmt.Println("Where should Hyper Stack be installed?")
	fmt.Println("This one folder will hold PHP, MySQL, phpMyAdmin, nginx and the rest,")
	fmt.Println(`plus a "www" folder: every project you put in www becomes <folder>.test.`)
	fmt.Println("Use a short path with plain English letters, for example", def)

	if st, err := os.Stdin.Stat(); err != nil || st.Mode()&os.ModeCharDevice == 0 {
		fmt.Println("No terminal to ask on, using", def)
		return def, nil
	}
	for {
		fmt.Printf("Install folder [%s]: ", def)
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("no folder chosen: %w", err)
		}
		dir := strings.Trim(strings.TrimSpace(line), `"`)
		if dir == "" {
			dir = def
		}
		if abs, verr := config.ValidateInstallDir(dir); verr != nil {
			fmt.Println("  ", verr)
			continue
		} else {
			return abs, nil
		}
	}
}
