/*-
 * Copyright © 2017, Jörg Pernfuß <code.jpe@gmail.com>
 * All rights reserved.
 *
 * Use of this source code is governed by a 2-clause BSD license
 * that can be found in the LICENSE file.
 */

package main // import "github.com/mjolnir42/zkonce"

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/client9/reopen"
	"github.com/droundy/goopt"
	"github.com/mjolnir42/erebos"
	"github.com/samuel/go-zookeeper/zk"
)

var startNode, finishNode, runLock string
var fromStart, fromFinish bool
var conf *Config
var logInitialized bool
var duration string

func init() {
	// Discard logspam from Zookeeper library
	erebos.DisableZKLogger()

	// set standard logger options
	erebos.SetLogrusOptions()
}

func main() {
	os.Exit(run())
}

func run() int {
	// parse command line flags
	cliConfPath := goopt.String([]string{`-c`, `--config`},
		`/etc/zkonce/zkonce.conf`, `Configuration file`)
	job := goopt.String([]string{`-j`, `--job`},
		``, `Job name to run command under`)
	per := goopt.String([]string{`-p`, `--per`},
		`day`, `Duration per which to run the command once`)
	start := goopt.Flag([]string{`-s`, `--from-start`}, []string{},
		`Calculate duration from last start timestamp (default)`, ``)
	finish := goopt.Flag([]string{`-f`, `--from-finish`}, []string{},
		`Calculate duration from last finish timestamp`, ``)
	goopt.Parse(nil)

	// validate cli input
	validXOR(start, finish)
	validJob(job)
	validDuration(per)

	// read runtime configuration
	conf = &Config{}
	if err := conf.FromFile(*cliConfPath); err != nil {
		logrus.Fatalf("Could not open configuration: %s", err)
	}

	// validate we can fork to the requested user
	validUser()
	validSyncGroup()

	// setup logfile
	if lfh, err := reopen.NewFileWriter(conf.LogFile); err != nil {
		logrus.Fatalf("Unable to open logfile: %s", err)
	} else {
		logrus.SetOutput(lfh)
		logInitialized = true
	}
	logrus.Infoln(`Starting zkonce`)

	conn, chroot := connect(conf.Ensemble)
	defer conn.Close()
	logrus.Infoln(`Configured zookeeper chroot:`, chroot)

	// ensure fixed node hierarchy exists
	if !zkHier(conn, filepath.Join(chroot, `zkonce`), true) {
		return 1
	}

	// ensure required nodes exist
	zkOncePath := filepath.Join(chroot, `zkonce`, conf.SyncGroup)
	if !zkCreatePath(conn, zkOncePath, true) {
		return 1
	}

	startNode = filepath.Join(zkOncePath, `start`)
	if !zkCreatePath(conn, startNode, true) {
		return 1
	}

	finishNode = filepath.Join(zkOncePath, `finish`)
	if !zkCreatePath(conn, finishNode, true) {
		return 1
	}

	runLock = filepath.Join(zkOncePath, `runlock`)
	if !zkCreatePath(conn, finishNode, true) {
		return 1
	}

	leaderChan, errChan := zkLeaderLock(conn)
	select {
	case <-errChan:
		return 1
	case <-leaderChan:
		leader(conn)
	}

	<-time.After(60 * time.Second)
	logrus.Infof("Shutting down")
	return 0
}

func leader(conn *zk.Conn) {
	fmt.Println("I AM THE LEADER")
	run := false

	val, s, err := conn.Get(startNode)
	assertOK(err)
	version := s.Version

	var startTime time.Time
	if len(val) > 0 {
		err = startTime.UnmarshalText(val)
		assertOK(err)
	}

	if startTime.IsZero() {
		fmt.Println(`START TIME IS ZERO`)
		run = true
	} else {
		diff := time.Now().UTC().Sub(startTime)
		if diff > time.Hour {
			run = true
		}
	}

	if !run {
		logrus.Infoln(`not running`)
		return
	}
	nowTime := time.Now().UTC().Format(time.RFC3339Nano)
	s, err = conn.Set(startNode, []byte(nowTime), version)
	assertOK(err)
	if s.Version > version {
		logrus.Infoln(`startNode version increased to`, s.Version)
	}

	cmdSlice := []string{}
	for i := range os.Args {
		if os.Args[i] == `--` {
			cmdSlice = os.Args[i+1:]
			break
		}
	}
	if len(cmdSlice) == 0 {
		return
	}
	cmd := exec.Command(cmdSlice[0], cmdSlice[1:]...)
	fmt.Println(`Running:`, cmdSlice)

	user, err := user.Lookup(`nobody`)
	assertOK(err)
	uid, _ := strconv.Atoi(user.Uid)
	gid, _ := strconv.Atoi(user.Gid)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid:    uint32(uid),
			Gid:    uint32(gid),
			Groups: []uint32{},
		},
	}
	out, err := cmd.Output()
	assertOK(err)
	fmt.Println(`Output:`, string(out))

	_, s, err = conn.Get(finishNode)
	assertOK(err)
	version = s.Version
	nowTime = time.Now().UTC().Format(time.RFC3339Nano)
	s, err = conn.Set(finishNode, []byte(nowTime), version)
	assertOK(err)
	if s.Version > version {
		logrus.Infoln(`finishNode version increased to`, s.Version)
	}
}

// vim: ts=4 sw=4 sts=4 noet fenc=utf-8 ffs=unix
