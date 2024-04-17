package main

import (
	"github.com/spf13/cobra"
	"github.com/x5iu/visc/cmd"
)

func main() {
	cobra.CheckErr(cmd.Command.Execute())
}
