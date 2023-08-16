package main

import (
	"fmt"
	"os"
)

func printUsageAndExit(exitCode int) {
	fmt.Println("Usage:")
	fmt.Println("    Help:")
	fmt.Println("        sft -h")
	fmt.Println("    Encryption:")
	fmt.Println("        sft <options> encrypt <file> ...")
	fmt.Println("        Options:")
	fmt.Println("            -p    set password (rarely needed)")
	fmt.Println("")
	fmt.Println("    Decryption:")
	fmt.Println("        sft <options> decrypt https://filetransfer.kpn.com/download/<uuid>#<base64key>")
	fmt.Println("        Options:")
	fmt.Println("            -s    show the list of files (do not download them) and exit")
	os.Exit(exitCode)
}

func parseOptions() ([]string, Options) {
	fmt.Println("")
	var options = Options{}
	i := 0
	var arg string
	for i, arg = range os.Args[1:] {
		if len(arg) != 2 {
			return os.Args[1+i:], options
		}

		if arg[0] == '-' {
			switch arg[1] {
			case 'p':
				{
					options.Password = true
				}
			case 's':
				{
					options.Show = true
				}
			case 'h':
				{
					options.Help = true
				}
			default:
				{
					fmt.Println("Unknown argument", arg)
					printUsageAndExit(1)
				}
			}
		} else {
			break
		}
	}
	return os.Args[1+i:], options
}

func parseMode(rem []string, options *Options) (encrypt bool, f []string) {
	if len(rem) < 2 {
		fmt.Println("Too few arguments")
		printUsageAndExit(1)
	}

	if rem[0] == "encrypt" {
		encrypt = true
	} else if rem[0] == "decrypt" {
		encrypt = false
	} else {
		fmt.Println("Unknown argument: ", rem[0])
		printUsageAndExit(1)
	}

	if !encrypt {
		if len(rem) != 2 {
			fmt.Println("Too many arguments for decryption")
			printUsageAndExit(1)
		}
	}
	return encrypt, rem[1:]
}

func main() {
	rem, options := parseOptions()
	if options.Help {
		printUsageAndExit(0)
	}
	encrypt, f := parseMode(rem, &options)

	if encrypt {
		url := encryptFiles(f, &options)
		fmt.Println("Succesfully encrypted and uplaoded the file(s)")
		fmt.Println("Download url:")
		fmt.Println(url)
	} else {
		decryptFromUrl(f[0], &options)
	}
}
