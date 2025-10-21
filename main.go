// Command ftc-sync provides a CLI to keep your current directory in sync with an FTC Robot via the OnBotJava api.
package main

import (
	"bufio"
	"encoding/json"
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
	"ping":      ping,
	"ping2":     ping2,
	"pull":      pull,
	"push":      push,
	"repl":      repl,
	"all":       all,
	"tree":      tree,
	"new":       newFile,
	"delete":    deleteFile,
	"templates": templates,
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
	debug = flag.Bool(
		"debug",
		false,
		"Print first N bytes of HTTP responses",
	)
)

func ping([]string) error {
	// is the robot available (repeats every second to ensure we stay in "connected devices list")
	fmt.Fprint(flag.CommandLine.Output(), "Pinging ... ")
	os.Stderr.Sync()
	res, err := client.PostForm("http://"+*remoteAddress+"/ping", url.Values{
		"name": []string{"ftc-sync/ping"},
	})
	if err != nil {
		return fmt.Errorf("POST: %w", err)
	}
	debugResponse(res)
	// TODO: decode the json response here
	fmt.Fprintln(flag.CommandLine.Output(), "DONE")
	return nil
}

func ping2(args []string) error {
	if err := ping(args); err != nil {
		return err
	}
	fmt.Fprint(flag.CommandLine.Output(), "Sleeping ... ")
	os.Stderr.Sync()
	time.Sleep(3 * time.Second)
	fmt.Fprintln(flag.CommandLine.Output(), "DONE")
	if err := ping(args); err != nil {
		return err
	}
	return nil
}

// <template-name> [<optional-file-name>]
func newFile(args []string) error {
	if len(args) != 1 && len(args) != 2 {
		return fmt.Errorf("invalid number of args: %d != (1,2)", len(args))
	}

	template := args[0]
	filename := template + ".java"
	if len(args) == 2 {
		filename = args[1]
	}

	vals := url.Values{
		"f": []string{"/src" + *remoteDirectory + filename},
	}
	form := url.Values{
		"new":               []string{"1"},
		"template":          []string{"templates/" + template},
		"opModeAnnotations": []string{"@TeleOp\n"}, // probably don't need the newline
		"teamName":          []string{},            // ???
	}

	res, err := client.PostForm("http://"+*remoteAddress+"/java/file/new?"+vals.Encode(), form)
	if err != nil {
		return fmt.Errorf("POST: %w", err)
	}
	defer res.Body.Close()
	debugResponse(res)

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("bad response code: %d != 200", res.StatusCode)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("unable to read body: %w", err)
	}
	if err := res.Body.Close(); err != nil {
		return fmt.Errorf("unable to close body: %w", err)
	}

	sbody := string(body)
	if sbody != vals["f"][0] {
		return fmt.Errorf("unexpected created file: %q != %q", sbody, vals["f"][0])
	}

	sbody = strings.Replace(sbody, "/src"+*remoteDirectory, "", 1)
	fmt.Fprintln(flag.CommandLine.Output(), sbody)

	return nil
}

// <file-name>
func deleteFile(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("invalid number of args: %d != 1", len(args))
	}

	name := args[0]

	// sanity check, should we check if the name is in the file listing?

	// `delete` form value is a JSON encoded array of strings
	// yes, I tested if this'll work without the json encoding... it doesn't

	files := []string{"src" + *remoteDirectory + name}

	toDelete, err := json.Marshal(files)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	form := url.Values{
		"delete": []string{string(toDelete)},
	}

	res, err := client.PostForm("http://"+*remoteAddress+"/java/file/delete", form)
	if err != nil {
		return fmt.Errorf("POST: %w", err)
	}
	defer res.Body.Close()
	debugResponse(res)

	// {"success": "true"}

	return nil
}

func templates([]string) error {
	res, err := client.Get("http://" + *remoteAddress + "/java/file/templates")
	if err != nil {
		return fmt.Errorf("GET: %w", err)
	}
	defer res.Body.Close()
	debugResponse(res)

	var templateList []struct {
		ExampleProject bool   `json:"exampleProject"`
		Name           string `json:"name"`
		Disabled       bool   `json:"disabled"`
		Autonomous     bool   `json:"autonomous"`
		Teleop         bool   `json:"teleop"`
		Example        bool   `json:"example"`
	}

	if err := json.NewDecoder(res.Body).Decode(&templateList); err != nil {
		return fmt.Errorf("json.decode: %w", err)
	}

	var buff [5]byte

	addBit := func(bit bool, off int) {
		if bit {
			buff[off] = '1'
		} else {
			buff[off] = '0'
		}
	}

	fmt.Fprintln(flag.CommandLine.Output(), "exampleProject,disabled,autonomous,teleop,example\tname")
	for _, item := range templateList {
		addBit(item.ExampleProject, 0)
		addBit(item.Disabled, 1)
		addBit(item.Autonomous, 2)
		addBit(item.Teleop, 3)
		addBit(item.Example, 4)

		fmt.Fprintln(
			flag.CommandLine.Output(),
			string(buff[:])+"\t"+item.Name,
		)
	}
	return nil
}

func all([]string) error {
	fmt.Fprintln(flag.CommandLine.Output(), "Reading Files...")
	res, err := client.Get("http://" + *remoteAddress + "/java/file/all")
	if err != nil {
		return fmt.Errorf("GET: %w", err)
	}
	defer res.Body.Close()
	debugResponse(res)

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
	defer res.Body.Close()
	debugResponse(res)

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
	defer res.Body.Close()
	debugResponse(res)

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
	defer res.Body.Close()
	debugResponse(res)

	return nil
}

func debugResponse(res *http.Response) {
	if !*debug {
		return
	}
	bits, err := httputil.DumpResponse(res, true)
	if err != nil {
		panic(err)
	}
	// warning: unicode unsafe byte slicing
	if len(bits) > 300 {
		bits = bits[:300]
	}
	fmt.Fprintln(flag.CommandLine.Output(), "-------------------------------")
	fmt.Fprintln(flag.CommandLine.Output(), string(bits))
	fmt.Fprintln(flag.CommandLine.Output(), "-------------------------------")
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
