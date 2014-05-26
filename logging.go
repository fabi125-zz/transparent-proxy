package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
)

type Priority int

const (
	LOG_EMERG Priority = iota
	LOG_ALERT
	LOG_CRIT
	LOG_ERR
	LOG_WARNING
	LOG_NOTICE
	LOG_INFO
	LOG_DEBUG
)

var priorityMap = map[Priority]string{
	LOG_EMERG:   "EMERG",
	LOG_ALERT:   "ALERT",
	LOG_CRIT:    "CRIT",
	LOG_ERR:     "ERR",
	LOG_WARNING: "WARNING",
	LOG_NOTICE:  "NOTICE",
	LOG_INFO:    "INFO",
	LOG_DEBUG:   "DEBUG",
}

var logFile *log.Logger
var logStderr *log.Logger
var minPriority = LOG_DEBUG

func addPriority(priority Priority, message string) string {
	return fmt.Sprintf("%s: %s", priorityMap[priority], message)
}

func addPrefix(prefix string, message string) string {
	return fmt.Sprintf("%s: %s", prefix, message)
}

// Print calls Output to print to the logFile.
// Arguments are handled in the manner of fmt.Print.
func Log(priority Priority, v ...interface{}) {
	if priority <= minPriority {
		logFile.Output(2, addPriority(priority, fmt.Sprint(v...)))
	}
}

// Printf calls Output to print to the logFile.
// Arguments are handled in the manner of fmt.Printf.
func Logf(priority Priority, format string, v ...interface{}) {
	if priority <= minPriority {
		logFile.Output(2, addPriority(priority, fmt.Sprintf(format, v...)))
	}
}

// Fatal is like Log() but also prints to stderr and is followed by a call to os.Exit(1).
func Fatal(v ...interface{}) {
	s := addPrefix("FATAL", fmt.Sprint(v...))
	logStderr.Output(2, s)
	logFile.Output(2, s)
	os.Exit(1)
}

// Fatalf is like to Logf() but also prints to stderr and is followed by a call to os.Exit(1).
func Fatalf(format string, v ...interface{}) {
	s := addPrefix("FATAL", fmt.Sprintf(format, v...))
	logStderr.Output(2, s)
	logFile.Output(2, s)
	os.Exit(1)
}


// Panic is like to Log() but also prints to stderr and is followed by a call to panic().
func Panic(v ...interface{}) {
	s := addPrefix("PANIC", fmt.Sprint(v...))
	logStderr.Output(2, s)
	logFile.Output(2, s)
	panic(s)
}

// Panicf is like to Logf() but also prints to stderr and is followed by a call to panic()
func Panicf(format string, v ...interface{}) {
	s := addPrefix("PANIC", fmt.Sprintf(format, v...))
	logStderr.Output(2, s)
	logFile.Output(2, s)
	panic(s)
}

func (p *Priority) String() string {
	value, ok := priorityMap[*p]
	if ok {
		return value
	}
	return "UNKNOWN"
}

func (p *Priority) Set(value string) error {
	input := strings.ToUpper(value)
	for key, value := range priorityMap {
		if value == input {
			*p = key
			return nil
		}
	}
	return errors.New(fmt.Sprintf("Unknown loglevel: %s", input))
}

func init() {
	filename := flag.String("logfile", "/var/log/proxy.log", "File for log messages")
	flag.Var(&minPriority, "loglevel", "Minimum log level")

	logFileHandle, err := os.OpenFile(*filename, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
	if err != nil {
		log.Fatal(err)
	}

	logPrefix := ""
	logFlags := log.Ldate|log.Lmicroseconds|log.Lshortfile
	
	logFile = log.New(logFileHandle, logPrefix, logFlags)
	logStderr = log.New(os.Stderr, logPrefix, logFlags)

}
