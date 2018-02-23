/*-
 * Copyright © 2017, Jörg Pernfuß <code.jpe@gmail.com>
 * All rights reserved.
 *
 * Use of this source code is governed by a 2-clause BSD license
 * that can be found in the LICENSE file.
 */

package main // import "github.com/mjolnir42/zkonce"

import (
	"bufio"
	"fmt"
	"io"
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

var startNode, finishNode, attemptNode, runLock string
var fromStart, fromFinish bool
var conf *Config
var logInitialized bool
var duration string
var zkonceVersion string

func init() {
	// Discard logspam from Zookeeper library
	erebos.DisableZKLogger()

	// set standard logger options
	erebos.SetLogrusOptions()

	// set goopt information
	goopt.Version = zkonceVersion
	goopt.Suite = `zkOnce`
	goopt.Summary = `Coordinate distributed command execution per duration`
	goopt.Author = `Jörg Pernfuß`
	goopt.Description = func() string {
		return "zkOnce can be used to coordinate the execution of a" +
			" command between multiple hosts.\n\nIt enforces that the" +
			" command only runs once per given time duration, either per" +
			" calendar day or per clock hour.\n\nThis means a per-day job" +
			" can run twice within seconds if the day changes in between" +
			" or similarly for per-hour if the hour changes."
	}
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
		assertOK(fmt.Errorf("Could not open configuration: %s", err))
	}

	// validate we can fork to the requested user
	validUser()
	validSyncGroup()

	// setup logfile
	var logFilePath string
	switch conf.LogPerJob {
	case true:
		logFilePath = filepath.Join(
			conf.LogPath,
			fmt.Sprintf("%s.log", *job),
		)
	default:
		logFilePath = conf.LogFile
	}
	if lfh, err := reopen.NewFileWriter(logFilePath); err != nil {
		assertOK(fmt.Errorf("Unable to open logfile: %s", err))
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

	zkOncePath = filepath.Join(zkOncePath, *job)
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

	attemptNode = filepath.Join(zkOncePath, `attempt`)
	if !zkCreatePath(conn, attemptNode, true) {
		return 1
	}

	runLock = filepath.Join(zkOncePath, `runlock`)
	if !zkCreatePath(conn, runLock, true) {
		return 1
	}

	leaderChan, errChan := zkLeaderLock(conn)

	block := make(chan error)
	select {
	case <-errChan:
		return 1
	case <-leaderChan:
		go leader(conn, block)
	}
	if errorOK(<-block) {
		return 1
	}
	if conf.BarrierEnabled {
		createBarrier(conf.BarrierFile)
	}
	logrus.Infof("Shutting down")
	return 0
}

func leader(conn *zk.Conn, block chan error) {
	logrus.Infoln("Leader election has been won")
	run := false

	var lastRun []byte
	var err error

	switch {
	case fromStart:
		lastRun, _, err = conn.Get(startNode)
	case fromFinish:
		lastRun, _, err = conn.Get(finishNode)
	}
	if sendError(err, block) {
		return
	}

	// the official time for this run
	now := time.Now().UTC()
	nowTime := now.Format(time.RFC3339Nano)

	// check if this run is eligible
	var lastTime time.Time
	if len(lastRun) > 0 {
		err = lastTime.UnmarshalText(lastRun)
		if sendError(err, block) {
			return
		}
	}

	if lastTime.IsZero() {
		run = true
	} else {
		nowYear, nowMonth, nowDay := now.UTC().Date()
		nowDate := time.Date(nowYear, nowMonth, nowDay,
			0, 0, 0, 0, time.UTC)

		lastYear, lastMonth, lastDay := lastTime.UTC().Date()
		lastDate := time.Date(lastYear, lastMonth, lastDay,
			0, 0, 0, 0, time.UTC)

		switch duration {
		case `day`:
			if nowDate.After(lastDate) {
				run = true
			}
		case `hour`:
			// it must be a different hour if it is a different day
			if nowDate.After(lastDate) {
				run = true
			} else if nowDate.Equal(lastDate) && now.UTC().Hour() > lastTime.UTC().Hour() {
				run = true
			}
		case `inf`:
		}
	}

	if !run {
		logrus.Infof("Not running since last run was at %s",
			lastTime.UTC().Format(time.RFC3339))
		close(block)
		return
	}

	// record time of attempted run
	if err = zkSet(conn, attemptNode, []byte(nowTime)); sendError(err, block) {
		return
	}

	cmdSlice := []string{}
	for i := range os.Args {
		if os.Args[i] == `--` {
			cmdSlice = os.Args[i+1:]
			break
		}
	}
	if len(cmdSlice) == 0 {
		close(block)
		return
	}
	var stderr io.ReadCloser
	cmd := exec.Command(cmdSlice[0], cmdSlice[1:]...)
	if stderr, err = cmd.StderrPipe(); sendError(err, block) {
		return
	}

	logrus.Infoln("Running command")

	if conf.User != `` {
		user, uerr := user.Lookup(conf.User)
		if sendError(uerr, block) {
			return
		}
		uid, uerr := strconv.Atoi(user.Uid)
		if sendError(uerr, block) {
			return
		}
		gid, uerr := strconv.Atoi(user.Gid)
		if sendError(uerr, block) {
			return
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Credential: &syscall.Credential{
				Uid:    uint32(uid),
				Gid:    uint32(gid),
				Groups: []uint32{},
			},
		}
	}

	// start the command
	if err = cmd.Start(); sendError(err, block) {
		return
	}

	// log command stderr output
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		logrus.Errorln(scanner.Text())
	}
	if err = scanner.Err(); err != nil {
		logrus.Errorln(err)
		// no return here, ensure cmd.Wait is called
	}

	if err = cmd.Wait(); sendError(err, block) {
		return
	}

	// write attemptTime as startTime
	if err = zkSet(conn, startNode, []byte(nowTime)); sendError(err, block) {
		return
	}

	// write fresh nowTime as finishTime
	nowTime = time.Now().UTC().Format(time.RFC3339Nano)
	if err = zkSet(conn, finishNode, []byte(nowTime)); sendError(err, block) {
		return
	}

	// unblock run()
	close(block)
}

// vim: ts=4 sw=4 sts=4 noet fenc=utf-8 ffs=unix
