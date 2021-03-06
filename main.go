package main

import (
	"flag"
	"github.com/howeyc/fsnotify"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

var (
	watcher  *fsnotify.Watcher
	watched  = make(map[string]struct{})
	exitCode = make(chan int)
	rootPath string

	buildQueued = true

	// flags
	afterAllOk string
	afterNotOk string
	verbose    bool
	buildArgs  string
	vetArgs    string
	testArgs   string
)

func runGoCmd(args ...string) (err error) {
	command := "go"

	// Use godep for godep based projects
	if _, err := os.Stat("Godeps/Godeps.json"); err == nil {
		args = append([]string{"go"}, args...)
		command = "godep"
	}

	return runCmd(command, args...)
}

func runCmd(name string, args ...string) (err error) {
	buf := new(commandBuffer)

	cmd := exec.Command(name, args...)
	cmd.Stdout = buf
	cmd.Stderr = buf
	cmd.Dir = rootPath

	if err = cmd.Run(); err != nil || verbose {
		print(buf.String())
	}

	return
}

func fullBuild() {
	var err error

	log.Println("glitch: building")
	if err = runGoCmd("build", buildArgs); err == nil {
		log.Println("glitch: build OK - vetting")

		if err = runGoCmd("vet", vetArgs); err == nil {
			log.Println("glitch: vet OK - testing")

			if err = runGoCmd("test", testArgs); err == nil {
				log.Println("glitch: test OK")

				if len(afterAllOk) > 0 {
					if err = runCmd("bash", "-c", afterAllOk); err == nil {
						log.Println("glitch: after-all-ok OK")
					}
				}
			}
		}
	}

	if err != nil && len(afterNotOk) > 0 {
		if err = runCmd("bash", "-c", afterNotOk); err == nil {
			log.Println("glitch: after-not-ok OK")
		}
	}

	if err != nil {
		log.Println("glitch: failed")
	} else {
		log.Println("glitch: all OK")
	}
}

func maybeQueueBuild(path string) {
	buildQueued = hasSuffix(path, ".go")
}

func handleCreate(path string) {
	watch(path)
	maybeQueueBuild(path)
}

func handleDelete(path string) {
	if _, watching := watched[path]; watching {
		_ = watcher.RemoveWatch(path)
		delete(watched, path)
	}
	maybeQueueBuild(path)
}

func handleModify(path string) {
	maybeQueueBuild(path)
}

func handleEvent(ev *fsnotify.FileEvent) {
	if len(ev.Name) > 0 {
		switch {
		case ev.IsCreate():
			handleCreate(ev.Name)
		case ev.IsDelete():
			handleDelete(ev.Name)
		case ev.IsModify():
			handleModify(ev.Name)
		}
	}
}

var (
	gitSuffix   = sprintf("%v.git", string(filepath.Separator))
	gitContains = sprintf("%v%v", gitSuffix, string(filepath.Separator))
)

func watch(dir string) {
	const watchFlags = fsnotify.FSN_CREATE | fsnotify.FSN_DELETE | fsnotify.FSN_MODIFY

	if _, watching := watched[dir]; watching {
		return
	}

	walker := func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if hasSuffix(path, gitSuffix) || contains(path, gitContains) {
			return nil
		}

		if fileInfo.IsDir() {
			if err = watcher.WatchFlags(path, watchFlags); err == nil {
				watched[path] = emptyStruct
			}
		}

		return err
	}

	_ = filepath.Walk(dir, walker)
}

func periodicallyLogWatchedCount() {
	logWatchedCount := func() {
		log.Printf("glitch: watching: %v paths", len(watched))
	}

	logWatchedCount()
	for _ = range time.Tick(5 * time.Second) {
		logWatchedCount()
	}
}

func periodicallyLogWatchedPaths() {
	logWatchedPaths := func() {
		log.Printf("glitch: watching: %v paths", len(watched))
		for path, _ := range watched {
			log.Println("glitch: watching:", path)
		}
	}

	logWatchedPaths()
	for _ = range time.Tick(5 * time.Second) {
		logWatchedPaths()
	}
}

func runEventLoop() {
	for {
		select {
		case ev := <-watcher.Event:
			handleEvent(ev)
		case err := <-watcher.Error:
			panicIf(err)
		}
	}
}

func runBuildLoop() {
	consumeBuildQueue := func() {
		if buildQueued {
			buildQueued = false
			clearScrollBuffer()
			fullBuild()
		}
	}

	for _ = range time.Tick(1 * time.Second) {
		go consumeBuildQueue()
	}
}

func main() {
	flag.StringVar(&afterAllOk, "after-all-ok", "", "command to run after build, vet and test succeed")
	flag.StringVar(&afterNotOk, "after-not-ok", "", "command to run after all OK")
	flag.StringVar(&buildArgs, "build", "./...", "arguments passed to `go build`")
	flag.StringVar(&testArgs, "test", "./...", "arguments passed to `go test`")
	flag.StringVar(&vetArgs, "vet", "./...", "arguments passed to `go vet`")
	flag.BoolVar(&verbose, "verbose", false, "be verbose")

	flag.Parse()

	wd, err := os.Getwd()
	panicIf(err)
	rootPath = wd

	w, err := fsnotify.NewWatcher()
	panicIf(err)
	watcher = w
	defer watcher.Close()

	//go periodicallyLogWatchedPaths()
	//go periodicallyLogWatchedCount()
	go runEventLoop()
	go runBuildLoop()

	watch(rootPath)
	os.Exit(<-exitCode)
}
