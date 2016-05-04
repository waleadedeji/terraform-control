package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"path/filepath"
	"github.com/hashicorp/otto/ui"
	"github.com/hashicorp/vault/helper/password"
	"github.com/mitchellh/cli"

)

var defaultInputReader io.Reader
var defaultInputWriter io.Writer
//TODO: This has to be retrieve dynamically form the env/url request

func NewUi(raw cli.Ui, env *Environment) ui.Ui {
	return &ui.Styled{
		Ui: &cliUi{
			CliUi: raw,
			env: env,
		},
	}
}

// cliUi is a wrapper around a cli.Ui that implements the otto.Ui
// interface. It is unexported since the NewUi method should be used
// instead.
type cliUi struct {
	CliUi cli.Ui
	env *Environment
	// Reader and Writer are used for Input
	Reader io.Reader
	Writer io.Writer

	interrupted bool
	l           sync.Mutex
}

func (u *cliUi) Header(msg string) {
	u.CliUi.Output(ui.Colorize(msg))
}

func (u *cliUi) Message(msg string) {
	u.CliUi.Output(ui.Colorize(msg))
}

func (u *cliUi) createFile() {
	// detect if file exists
	path := filepath.Join(GetDataFolder(), "/repo-" + u.env.Name, u.env.Path, "/planOutput")
	var _, err = os.Stat(path)

	// create file if not exists
	if os.IsNotExist(err) {
		var file, err = os.Create(path)
		check(err)
		defer file.Close()
	}
}

func check(e error) {
    if e != nil {
        panic(e)
    }
}

// TODO: this is a hacky, hacky
// to write into a file the terraform output by reusing
// github.com/mitchellh/cli and github.com/hashicorp/otto/ui
// We probably want to write our own output capture funcionality
func (u *cliUi) Raw(msg string) {
	u.createFile()
	var file, err = os.OpenFile(filepath.Join(GetDataFolder(), "/repo-" + u.env.Name, u.env.Path, "/planOutput"), os.O_APPEND|os.O_RDWR, 0644)
	check(err)

	defer file.Close()
    check(err)

    _, err = file.WriteString(msg)
    fmt.Print(msg)
    if err != nil {
    	fmt.Print(err)
    }

    err = file.Sync()
	check(err)
}

func (i *cliUi) Input(opts *ui.InputOpts) (string, error) {
	// If any of the configured EnvVars are set, we don't ask for input.
	if value := opts.EnvVarValue(); value != "" {
		return value, nil
	}

	r := i.Reader
	w := i.Writer
	if r == nil {
		r = defaultInputReader
	}
	if w == nil {
		w = defaultInputWriter
	}
	if r == nil {
		r = os.Stdin
	}
	if w == nil {
		w = os.Stdout
	}

	// Make sure we only ask for input once at a time. Terraform
	// should enforce this, but it doesn't hurt to verify.
	i.l.Lock()
	defer i.l.Unlock()

	// If we're interrupted, then don't ask for input
	if i.interrupted {
		return "", errors.New("interrupted")
	}

	// Listen for interrupts so we can cancel the input ask
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	// Build the output format for asking
	var buf bytes.Buffer
	buf.WriteString("[reset]")
	buf.WriteString(fmt.Sprintf("[bold]%s[reset]\n", opts.Query))
	if opts.Description != "" {
		s := bufio.NewScanner(strings.NewReader(opts.Description))
		for s.Scan() {
			buf.WriteString(fmt.Sprintf("  %s\n", s.Text()))
		}
		buf.WriteString("\n")
	}
	if opts.Default != "" {
		buf.WriteString("  [bold]Default:[reset] ")
		buf.WriteString(opts.Default)
		buf.WriteString("\n")
	}
	buf.WriteString("  [bold]Enter a value:[reset] ")

	// Ask the user for their input
	if _, err := fmt.Fprint(w, ui.Colorize(buf.String())); err != nil {
		return "", err
	}

	// Listen for the input in a goroutine. This will allow us to
	// interrupt this if we are interrupted (SIGINT)
	result := make(chan string, 1)
	if opts.Hide {
		f, ok := r.(*os.File)
		if !ok {
			return "", fmt.Errorf("reader must be a file")
		}

		line, err := password.Read(f)
		if err != nil {
			return "", err
		}

		result <- line
	} else {
		go func() {
			var line string
			if _, err := fmt.Fscanln(r, &line); err != nil {
				log.Printf("[ERR] UIInput scan err: %s", err)
			}

			result <- line
		}()
	}

	select {
	case line := <-result:
		fmt.Fprint(w, "\n")

		if line == "" {
			line = opts.Default
		}

		return line, nil
	case <-sigCh:
		// Print a newline so that any further output starts properly
		// on a new line.
		fmt.Fprintln(w)

		// Mark that we were interrupted so future Ask calls fail.
		i.interrupted = true

		return "", errors.New("interrupted")
	}
}