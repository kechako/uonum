package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/kechako/uonum"
	"github.com/pkg/errors"
)

var (
	dbName  string
	verbose bool
)

func init() {
	flag.StringVar(&dbName, "db", filepath.Join(getUserHome(), "nonum.db"), "Database path.")
	flag.BoolVar(&verbose, "v", false, "Verbose messages.")

	flag.Usage = func() {
		fmt.Fprint(os.Stderr, `
Usage:
    uonum [options] register [input file]
    uonum [options] generate [trigger word]
    uonum [options] dump

Options:
`)
		flag.PrintDefaults()
		fmt.Fprintln(os.Stderr)
	}
}

type runner func(args []string) (int, error)

func main() {
	flag.Parse()
	if flag.NArg() == 0 {
		printHelp()
	}

	cmd := flag.Args()[0]
	args := flag.Args()[1:]

	var r runner

	switch cmd {
	case "register":
		r = register
	case "generate":
		r = generate
	case "dump":
		r = dump
	default:
		printHelp()
	}

	if code, err := r(args); err != nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "%+v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "%v\n", err)
		}

		os.Exit(code)
	}
}

func printHelp() {
	flag.Usage()
	os.Exit(1)
}

func getUserHome() string {
	home := os.Getenv("HOME")
	if home == "" {
		home = os.Getenv("USERPROFILE")
	}

	return home
}

func register(args []string) (int, error) {
	g := uonum.New()
	err := g.Open(dbName)
	if err != nil {
		return 1, err
	}
	defer g.Close()

	var r io.Reader
	if len(args) > 0 {
		file, err := os.Open(args[0])
		if err != nil {
			return 1, errors.Wrapf(err, "Could not open the input file [%s].", args[0])
		}
		defer file.Close()
		r = file
	} else {
		r = os.Stdin
	}

	s := bufio.NewScanner(r)
	for s.Scan() {
		err = g.Register(s.Text())
		if err != nil {
			return 1, err
		}
	}
	err = s.Err()
	if err != nil {
		return 1, err
	}

	return 0, nil
}

func dump(args []string) (int, error) {
	g := uonum.New()
	err := g.Open(dbName)
	if err != nil {
		return 1, err
	}
	defer g.Close()

	buf := bufio.NewWriter(os.Stdout)
	defer buf.Flush()

	err = g.Dump(buf)
	if err != nil {
		return 1, nil
	}

	return 0, nil
}

func generate(args []string) (int, error) {
	g := uonum.New()
	err := g.Open(dbName)
	if err != nil {
		return 1, err
	}
	defer g.Close()

	var trig string
	if len(args) > 0 {
		trig = args[0]
	} else {
		for trig == "" {
			fmt.Print("Trigger word > ")
			if _, err := fmt.Scanln(&trig); err != nil {
				return 1, errors.Wrap(err, "Could not read trigger word.")
			}
		}
	}

	text, err := g.Generate(trig)
	if err != nil {
		return 1, err
	}

	fmt.Println(text)

	return 0, nil
}
