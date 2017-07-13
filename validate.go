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
	"os/user"
	"strconv"

	"github.com/Sirupsen/logrus"
	"github.com/davecgh/go-spew/spew"
)

func validDuration(per *string) {
	switch *per {
	case `day`, `hour`, `inf`:
		duration = *per
	default:
		assertOK(fmt.Errorf("Invalid per duration -p|--per spec: %s", *per))
	}
}

func validJob(job *string) {
	if *job == `` {
		assertOK(fmt.Errorf(`Invalid empty jobname -j|--job`))
	}
}

func validSyncGroup() {
	if conf.SyncGroup == `` {
		assertOK(fmt.Errorf(`Invalid empty sync.group in configuration`))
	}
}

func validXOR(start, finish *bool) {
	if *start && *finish {
		assertOK(fmt.Errorf(`Can not start both from start and finish timestamp`))
	}

	// set default
	if !*start && !*finish {
		fromStart = true
		return
	}
	fromStart = *start
	fromFinish = *finish
}

func validUser() {
	// no user specified - run as current user
	if conf.User == `` {
		return
	}
	uidCurrent := os.Getuid()
	userJob, err := user.Lookup(conf.User)
	assertOK(err)
	uidJob, err := strconv.Atoi(userJob.Uid)
	assertOK(err)
	// same user is not a problem
	if uidCurrent == uidJob {
		return
	}
	if uidCurrent != 0 {
		assertOK(fmt.Errorf("Can only switch to %s(%d) as root", userJob.Username, uidJob))
	}
}

func assertOK(err error) {
	if err != nil {
		if logInitialized {
			spew.Fdump(os.Stderr, err)
			logrus.Fatalf("%s", err.Error())
		}
		earlyAbort(fmt.Sprintf("%s", err.Error()))
	}
}

func earlyAbort(str string) {
	fmt.Fprintln(os.Stderr, str)
	os.Exit(1)
}

func errorOK(err error) bool {
	if err != nil {
		logrus.Errorln(err)
		return true
	}
	return false
}

func sendError(err error, c chan error) bool {
	if err != nil {
		c <- err
		return true
	}
	return false
}

// vim: ts=4 sw=4 sts=4 noet fenc=utf-8 ffs=unix
