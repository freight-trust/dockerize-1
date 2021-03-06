package main

import (
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"path"
	"runtime"
	"strings"
	"time"
)

const (
	schemeFile  = "file"
	schemeTCP   = "tcp"
	schemeTCP4  = "tcp4"
	schemeTCP6  = "tcp6"
	schemeUnix  = "unix"
	schemeHTTP  = "http"
	schemeHTTPS = "https"
)

// Read-only globals for use only within init() and main().
//nolint:gochecknoglobals
var (
	app = strings.TrimSuffix(path.Base(os.Args[0]), ".test")
	ver = "unknown" // set by ./release
	cfg struct {
		version       bool
		ini           iniConfig
		templatePaths stringsFlag // file or file:file or dir or dir:dir
		template      templateConfig
		waitURLs      urlsFlag
		wait          waitConfig
		tailStdout    stringsFlag
		tailStderr    stringsFlag
	}
)

// One-time initialization shared with tests.
func init() { // nolint:gochecknoinits
	flag.BoolVar(&cfg.version, "version", false, "print version and exit")
	flag.StringVar(&cfg.ini.source, "env", "", "path or URL to INI file with default values for unset env vars")
	flag.BoolVar(&cfg.ini.options.AllowPythonMultilineValues, "multiline", false, "allow Python-like multi-line values in INI file")
	flag.StringVar(&cfg.ini.section, "env-section", "", "section name in INI file")
	flag.Var(&cfg.ini.headers, "env-header", "`name:value` or path to file containing name:value for HTTP header to send\n(if -env is an URL)\ncan be passed multiple times")
	flag.Var(&cfg.templatePaths, "template", "template `src:dst` file or dir paths, :dst part is optional\ncan be passed multiple times")
	flag.BoolVar(&cfg.template.noOverwrite, "no-overwrite", false, "do not overwrite existing destination file from template")
	flag.BoolVar(&cfg.template.strict, "template-strict", false, "fail if template mention unset environment variable")
	flag.Var(&cfg.template.delims, "delims", "action delimiters in templates")
	flag.Var(&cfg.waitURLs, "wait", "wait for `url` (file/tcp/tcp4/tcp6/unix/http/https)\ncan be passed multiple times")
	flag.Var(&cfg.wait.headers, "wait-http-header", "`name:value` for HTTP header to send\n(if -wait use HTTP)\ncan be passed multiple times")
	flag.BoolVar(&cfg.wait.skipTLSVerify, "skip-tls-verify", false, "skip TLS verification for HTTPS -wait and -env urls")
	flag.BoolVar(&cfg.wait.skipRedirect, "wait-http-skip-redirect", false, "do not follow HTTP redirects\n(if -wait use HTTP)")
	flag.Var(&cfg.wait.statusCodes, "wait-http-status-code", "HTTP status `code` to wait for (2xx by default)\ncan be passed multiple times")
	flag.DurationVar(&cfg.wait.timeout, "timeout", 10*time.Second, "timeout for -wait")
	flag.DurationVar(&cfg.wait.delay, "wait-retry-interval", time.Second, "delay before retrying failed -wait")
	flag.Var(&cfg.tailStdout, "stdout", "file `path` to tail to stdout\ncan be passed multiple times")
	flag.Var(&cfg.tailStderr, "stderr", "file `path` to tail to stderr\ncan be passed multiple times")

	flag.Usage = usage
}

func main() { // nolint:gocyclo,gocognit
	if !flag.Parsed() { // flags may be already parsed by tests
		flag.Parse()
	}

	var iniURL, iniHTTP, templatePathBad, waitBadScheme, waitHTTP bool
	if u, err := url.Parse(cfg.ini.source); err == nil && u.IsAbs() {
		iniURL = true
		iniHTTP = u.Scheme == schemeHTTP || u.Scheme == schemeHTTPS
	}
	for _, path := range cfg.templatePaths {
		parts := strings.Split(path, ":")
		templatePathBad = templatePathBad || path == "" || parts[0] == "" || len(parts) > 2
	}
	for _, u := range cfg.waitURLs {
		switch u.Scheme {
		case schemeFile, schemeTCP, schemeTCP4, schemeTCP6, schemeUnix:
		case schemeHTTP, schemeHTTPS:
			waitHTTP = true
		default:
			waitBadScheme = true
		}
	}
	switch {
	case flag.NArg() == 0 && flag.NFlag() == 0:
		flag.Usage()
		os.Exit(2)
	case iniURL && !iniHTTP:
		fatalFlagValue("scheme must be http/https", "env", cfg.ini.source)
	case len(cfg.ini.headers) > 0 && !iniHTTP:
		fatalFlagValue("require -env with HTTP url", "env-header", cfg.ini.headers)
	case templatePathBad:
		fatalFlagValue("require src:dst or src", "template", cfg.templatePaths)
	case cfg.template.noOverwrite && len(cfg.templatePaths) == 0:
		fatalFlagValue("require -template", "no-overwrite", cfg.template.noOverwrite)
	case cfg.template.strict && len(cfg.templatePaths) == 0:
		fatalFlagValue("require -template", "template-strict", cfg.template.strict)
	case cfg.template.delims[0] != "" && len(cfg.templatePaths) == 0:
		fatalFlagValue("require -template", "delims", cfg.template.delims)
	case waitBadScheme:
		fatalFlagValue("scheme must be file/tcp/tcp4/tcp6/unix/http/https", "wait", cfg.waitURLs)
	case len(cfg.wait.headers) > 0 && !waitHTTP:
		fatalFlagValue("require -wait with HTTP url", "wait-http-header", cfg.wait.headers)
	case len(cfg.wait.statusCodes) > 0 && !waitHTTP:
		fatalFlagValue("require -wait with HTTP url", "wait-http-status-code", cfg.wait.statusCodes)
	case cfg.wait.skipRedirect && !waitHTTP:
		fatalFlagValue("require -wait with HTTP url", "wait-http-skip-redirect", cfg.wait.skipRedirect)
	case cfg.wait.skipTLSVerify && !iniHTTP && !waitHTTP:
		fatalFlagValue("require -wait/-env with HTTP url", "skip-tls-verify", cfg.wait.skipTLSVerify)
	case cfg.version:
		fmt.Println(app, ver, runtime.Version())
		os.Exit(0)
	}

	cfg.ini.skipTLSVerify = cfg.wait.skipTLSVerify
	if cfg.template.delims[0] == "" {
		cfg.template.delims = [2]string{"{{", "}}"}
	}

	defaultEnv, err := loadINISection(cfg.ini)
	if err != nil {
		fatal("Failed to load INI: %s.", err)
	}

	setDefaultEnv(defaultEnv)

	cfg.template.data.Env = getEnv()
	err = processTemplatePaths(cfg.template, cfg.templatePaths)
	if err != nil {
		fatal("Failed to process templates: %s.", err)
	}

	err = waitForURLs(cfg.wait, cfg.waitURLs)
	if err != nil {
		fatal("Failed to wait: %s.", err)
	}

	for _, path := range cfg.tailStdout {
		tailFile(path, os.Stdout)
	}
	for _, path := range cfg.tailStderr {
		tailFile(path, os.Stderr)
	}

	switch {
	case flag.NArg() > 0:
		code, err := runCmd(flag.Arg(0), flag.Args()[1:]...)
		if err != nil {
			fatal("Failed to run command: %s.", err)
		}
		os.Exit(code)
	case len(cfg.tailStdout)+len(cfg.tailStderr) > 0:
		select {}
	}
}

func warnIfFail(f func() error) {
	if err := f(); err != nil {
		log.Printf("Warning: %s.", err)
	}
}

func fatal(format string, v ...interface{}) {
	log.Printf(format, v...)
	os.Exit(123)
}

func usage() {
	fmt.Println(`Usage:
  dockerize options [ command [ arg ... ] ]
  dockerize [ options ] command [ arg ... ]

Utility to simplify running applications in docker containers.

Options:`)
	flag.PrintDefaults()
	fmt.Println()
	fmt.Println(`Example: Generate /etc/nginx/nginx.conf using nginx.tmpl as a template, tail nginx logs, wait for a website to become available on port 8000 and then start nginx.`)
	fmt.Println(`
   dockerize -template nginx.tmpl:/etc/nginx/nginx.conf \
             -stdout /var/log/nginx/access.log \
             -stderr /var/log/nginx/error.log \
             -wait tcp://web:8000 \
             nginx -g 'daemon off;'
	`)
	fmt.Println(`For more information, see https://github.com/powerman/dockerize`)
}
