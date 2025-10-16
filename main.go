// Command ftc-sync provides a CLI to keep your current directory in sync with an FTC Robot via the OnBotJava api.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"slices"
	"sort"
	"strings"
	"time"
)

var client = &http.Client{
	Jar:     newFsJar(".cookies"),
	Timeout: time.Second, // seems reasonable
}

var cmds = map[string]func([]string) error{
	"ping":  ping,
	"ping2": ping2,
	"pull":  pull,
	"push":  push,
	"repl":  repl,
	"all":   all,
	"tree":  tree,
}

func envStr(envVar, blankValue string) string {
	if val, ok := os.LookupEnv(envVar); ok {
		return val
	}
	return blankValue
}

var (
	remoteDirectory = flag.String(
		"remote",
		envStr("FTC_REMOTE_DIRECTORY", "/org/firstinspires/ftc/teamcode/"),
		"Directory on remote system (FTC_REMOTE_DIRECTORY)",
	)
	remoteAddress = flag.String(
		"address",
		envStr("FTC_ROBOT_ADDRESS", "192.168.49.1:8080"),
		"Host:Port of the robot to connect to (FTC_ROBOT_ADDRESS)",
	)
)

func ping([]string) error {
	// is the robot available (repeats every second to ensure we stay in "connected devices list")
	// TODO: timeout at 1 second
	fmt.Fprintln(flag.CommandLine.Output(), "Pinging...")
	res, err := client.PostForm("http://"+*remoteAddress+"/ping", url.Values{
		"name": []string{"ftc-sync/ping"},
	})
	if err != nil {
		return fmt.Errorf("POST: %w", err)
	}
	bits, err := httputil.DumpResponse(res, true)
	if err != nil {
		return fmt.Errorf("DUMP: %w", err)
	}
	fmt.Fprintln(flag.CommandLine.Output(), string(bits))
	return nil
}

func ping2(args []string) error {
	if err := ping(args); err != nil {
		return err
	}
	time.Sleep(3 * time.Second)
	if err := ping(args); err != nil {
		return err
	}
	return nil
}

func repl([]string) error {
	return fmt.Errorf(`repl: %w`, errors.ErrUnsupported)
}

func all([]string) error {
	fmt.Fprintln(flag.CommandLine.Output(), "Reading Files...")
	res, err := client.Get("http://" + *remoteAddress + "/java/file/all")
	if err != nil {
		return fmt.Errorf("GET: %w", err)
	}

	// bits, err := httputil.DumpResponse(res, true)
	// if err != nil {
	// 	return fmt.Errorf("DUMP: %w", err)
	// }
	// fmt.Fprintln(flag.CommandLine.Output(), string(bits[:300]))

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %s", res.Status)
	}

	var boundary string
	scan := bufio.NewScanner(res.Body)
	for scan.Scan() {
		if boundary != "" {
			break // scanning one line past boundary (got skip blank line; this should be after the boundary assignment)
		}
		boundary = scan.Text()
	}
	slog.Debug("got boundary", slog.String("boundary", boundary))
	// var myName string
	for scan.Scan() {
		line := scan.Text()
		fileName, found := strings.CutPrefix(line, boundary+" ")
		if found {
			shorter, found := strings.CutPrefix(fileName, *remoteDirectory)
			if !found {
				slog.Warn("file outside -remote", slog.String("name", fileName))
				continue
			}
			// myName = fileName
			slog.Info("TODO creating file", slog.String("name", shorter))
			continue
		}
		// slog.Info("TODO adding to file", slog.String("name", myName), slog.String("line", line))
	}
	if err := scan.Err(); err != nil {
		panic(err)
	}
	return nil
}

func tree([]string) error {
	slog.Debug("Fetching tree...")
	res, err := client.Get("http://" + *remoteAddress + "/java/file/tree")
	if err != nil {
		return fmt.Errorf("GET: %w", err)
	}

	// bits, err := httputil.DumpResponse(res, true)
	// if err != nil {
	// 	return fmt.Errorf("DUMP: %w", err)
	// }
	// fmt.Fprintln(flag.CommandLine.Output(), string(bits[:300]))

	var myTree struct {
		Sources []string `json:"src"`
	}
	if err := json.NewDecoder(res.Body).Decode(&myTree); err != nil {
		panic(err)
	}

	// remove directories
	files := slices.DeleteFunc(myTree.Sources, func(s string) bool {
		return strings.HasSuffix(s, "/")
	})

	// remove files outside our remoteDirectory (and remove that prefix)
	newFiles := make([]string, 0, len(files))
	for _, file := range files {
		if name, found := strings.CutPrefix(file, *remoteDirectory); found {
			newFiles = append(newFiles, name)
		}
	}
	files = newFiles

	slog.Debug("got tree", slog.Any("files", files))

	for _, file := range files {
		fmt.Fprintln(flag.CommandLine.Output(), file)
	}

	return nil
}

func pull(args []string) error {
	if len(args) != 1 {
		flag.Usage()
		os.Exit(1)
	}
	file := args[0]
	slog.Debug("fetching file", slog.String("file", file))

	res, err := client.Get("http://" + *remoteAddress + "/java/file/get?f=/src" + *remoteDirectory + file)
	if err != nil {
		return fmt.Errorf("GET: %w", err)
	}

	// bits, err := httputil.DumpResponse(res, true)
	// if err != nil {
	// 	return fmt.Errorf("DUMP: %w", err)
	// }
	// fmt.Fprintln(flag.CommandLine.Output(), string(bits[:300]))
	bits, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("ReadAll: %w", err)
	}

	fmt.Fprint(os.Stdout, string(bits))

	return nil
}

// <filename> <location-on-disk;dash-if-stdin;or-omitted-if-the-same-locally>
func push(args []string) error {
	if len(args) == 0 || len(args) > 2 {
		flag.Usage()
		os.Exit(1)
	}
	remoteFile := args[1]
	var in io.ReadCloser
	var err error
	if len(args) == 2 && args[1] == "-" {
		in = io.NopCloser(os.Stdin)
	} else if len(args) == 2 {
		in, err = os.Open(args[1])
	} else {
		in, err = os.Open(args[0])
	}
	if err != nil {
		return fmt.Errorf("failed opening: %w", err)
	}

	contents, err := io.ReadAll(in)
	if err != nil {
		return fmt.Errorf("read all: %w", err)
	}

	res, err := client.PostForm("http://"+*remoteAddress+"/java/file/get?f=/src"+*remoteDirectory+remoteFile, url.Values{
		"data": {string(contents)},
	})
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}

	bits, err := httputil.DumpResponse(res, true)
	if err != nil {
		return fmt.Errorf("DUMP: %w", err)
	}
	fmt.Fprintln(flag.CommandLine.Output(), string(bits))

	return nil
}

func usage() {
	out := flag.CommandLine.Output()
	subcommands := slices.Collect(maps.Keys(cmds))
	sort.Strings(subcommands)
	fmt.Fprintf(out, "Usage of %s %s:\n", os.Args[0], subcommands)
	fmt.Fprintf(out, "\nSubcommands:\n")
	for _, cmd := range subcommands {
		fmt.Fprintf(out, "\t%s: TODO\n", cmd)
	}
	fmt.Fprintln(out)
	flag.PrintDefaults()
}

func main() {
	flag.Usage = usage
	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		os.Exit(1)
	}
	cmd, ok := cmds[args[0]]
	if !ok {
		flag.Usage()
		os.Exit(1)
	}
	if err := cmd(args[1:]); err != nil {
		fmt.Fprintf(flag.CommandLine.Output(), "%s\n", err.Error())
		os.Exit(1)
	}
}
