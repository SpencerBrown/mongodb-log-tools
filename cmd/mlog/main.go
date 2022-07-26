package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/SpencerBrown/mongodb-log-tools/info"
)

func main() {

	flag.Usage = func() {
		cmdName := os.Args[0]
		fmt.Fprintf(flag.CommandLine.Output(), "Usage of %s:\n", cmdName)
		flag.PrintDefaults()
		fmt.Fprintf(flag.CommandLine.Output(), "Subcommands: foo, bar\nRun %s <subcommand> --help for usage information.\n", cmdName)
	}

	genericVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *genericVersion {
		fmt.Println(version())
		return
	}

	if len(flag.Args()) < 1 {
		flag.Usage()
		os.Exit(3)
	}

	subcommand := flag.Args()[0]
	subflags := flag.Args()[1:]

	switch subcommand {
	case "info":
		infoCmd := flag.NewFlagSet("info", flag.ExitOnError)
		infoCmd.Parse(subflags)
		nFiles := infoCmd.NArg()
		if nFiles <= 0 {
			fmt.Printf("Log file name required: 'mlog info <filename>'\n")
			os.Exit(3)
		}
		for iFile := 0; iFile < nFiles; iFile++ {
			logFile := infoCmd.Arg(iFile)
			fmt.Printf("\n--------START LOG FILE: %s-----------\n", logFile)
			err := info.List(logFile)
			if err != nil {
				fmt.Printf("mlog info error: %v\n", err)
			}
			fmt.Printf("\n--------END LOG FILE: %s-----------\n", logFile)
		}
	}
}
