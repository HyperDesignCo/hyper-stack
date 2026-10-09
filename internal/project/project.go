// Package project reads and validates hyper.json.
package project

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Database describes the project's database.
type Database struct {
	Engine string `json:"engine"`
	Name   string `json:"name"`
	Seed   bool   `json:"seed"`
}

// Config is the parsed hyper.json.
type Config struct {
	Name       string   `json:"name"`
	Domain     string   `json:"domain"`
	PHP        string   `json:"php"`
	Extensions []string `json:"extensions"`
	Database   Database `json:"database"`
	Services   []string `json:"services"`
	Node       string   `json:"node"`
	Setup      []string `json:"setup"`
	Workers    []string `json:"workers"`
}

var (
	nameRe   = regexp.MustCompile(`^[a-z0-9-]+$`)
	dbNameRe = regexp.MustCompile(`^[a-z0-9_]+$`)

	phpVersions = []string{"8.2", "8.3", "8.4"}
	services    = []string{"redis", "mailpit"}
)

// ValidationError lists every invalid field.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return "invalid hyper.json:\n  - " + strings.Join(e.Problems, "\n  - ")
}

// Load reads and validates the file at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w. Run this command from a project folder that contains hyper.json", path, err)
	}
	return Parse(data)
}

// Parse decodes and validates hyper.json content. Unknown fields are errors.
func Parse(data []byte) (*Config, error) {
	var c Config
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("invalid hyper.json: %w", err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Validate applies defaults and checks every field.
func (c *Config) Validate() error {
	var p []string
	add := func(f, msg string) { p = append(p, f+": "+msg) }

	if c.Name == "" {
		add("name", "is required")
	} else if !nameRe.MatchString(c.Name) {
		add("name", fmt.Sprintf("%q must match ^[a-z0-9-]+$", c.Name))
	}
	if c.Domain == "" && c.Name != "" {
		c.Domain = c.Name + ".test"
	}
	if c.Domain != "" && !strings.HasSuffix(c.Domain, ".test") {
		add("domain", fmt.Sprintf("%q must end with .test", c.Domain))
	}
	if c.PHP == "" {
		add("php", "is required")
	} else if !contains(phpVersions, c.PHP) {
		add("php", fmt.Sprintf("%q is not supported, use one of %s", c.PHP, strings.Join(phpVersions, ", ")))
	}
	if c.Database.Engine == "" {
		add("database.engine", "is required")
	} else if c.Database.Engine != "mysql" {
		add("database.engine", fmt.Sprintf("%q is not supported, only mysql", c.Database.Engine))
	}
	if c.Database.Name == "" {
		add("database.name", "is required")
	} else if !dbNameRe.MatchString(c.Database.Name) {
		add("database.name", fmt.Sprintf("%q must match ^[a-z0-9_]+$", c.Database.Name))
	}
	for i, s := range c.Services {
		if !contains(services, s) {
			add(fmt.Sprintf("services[%d]", i), fmt.Sprintf("%q is not supported, use one of %s", s, strings.Join(services, ", ")))
		}
	}
	if c.Node == "" {
		c.Node = "20"
	}
	if len(p) > 0 {
		return &ValidationError{Problems: p}
	}
	return nil
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
