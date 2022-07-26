package info

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func List(fileName string) error {
	logFile, err := os.Open(fileName)
	if err != nil {
		return fmt.Errorf("error opening log file '%s': %v", fileName, err)
	}
	var earliest, latest time.Time
	var firstTime bool = true
	var startupInfo startupInfoT
	// Read structured log file line by line
	perLine := bufio.NewScanner(logFile)
	lineCount := 0
	for perLine.Scan() {
		logLine, err := logLine(perLine.Bytes(), &startupInfo)
		lineCount++
		if logLine == nil {
			continue // skippable line
		}
		if err != nil {
			return fmt.Errorf("error in line from log file '%s': %v\nLine is: %s", fileName, err, perLine.Text())
		}
		if firstTime {
			firstTime = false
			earliest = logLine.timeStamp
			latest = logLine.timeStamp
		} else {
			if logLine.timeStamp.Before(earliest) {
				earliest = logLine.timeStamp
			}
			if logLine.timeStamp.After(latest) {
				latest = logLine.timeStamp
			}
		}
		if startupInfo.complete {
			printStartup(&startupInfo)
		}
	}
	if err := perLine.Err(); err != nil {
		return fmt.Errorf("error reading log file '%s': %v", fileName, err)
	}
	fmt.Printf("%d lines in log file %s\n", lineCount, fileName)
	_, tzo := earliest.Zone()
	fmt.Printf("Log file timezone is UTC %d hours %d minutes)\n", tzo/3600, tzo%60)
	fmt.Printf("UTC time range in log file: %s -to- %s (%s)\n", earliest.UTC().Format(time.ANSIC), latest.UTC().Format(time.ANSIC), latest.Sub(earliest))
	return nil
}

// {"t":{"$date":"2022-07-20T12:29:51.886-07:00"},"s":"I",  "c":"CONTROL",  "id":20721,   "ctx":"conn40413","msg":"Process Details","attr":{"pid":"16875","port":27017,"architecture":"64-bit","host":"pd3lon-mdb-07"}}'

// logJSONT is a struct matching the JSON format of a structured log line
type logJSONT struct {
	T struct {
		Date string `json:"$date"`
	} // Timestamp
	S         string   // Severity
	C         string   // Component
	CTX       string   // Context
	ID        int      // Unique ID
	MSG       string   // Message body
	Attr      any      // Optional: Additional attributes
	Tags      []string // Optional: array of tags
	Truncated any      // If truncated: truncation information
	Size      int      // If truncated: original size of log line
}

// {"t":{"$date":  "2022-07-20T12:29:51.886-07:00"}...}
const timeLayout = "2006-01-02T15:04:05.999-07:00"

// logT is a struct with decoded/interpreted fields from a log line

type logT struct {
	timeStamp time.Time
}

const skippingLines = "HEADER INCLUDED, NOW SKIPPING"

// startupInfoT is a struct that contains all the startup information from a log file
type startupInfoT struct {
	isStartup         bool // flag that this is an actual startup, not just a log rotation
	complete          bool // flag that we've filled in all the info
	timeStamp         time.Time
	processID         int
	port              int
	dbPath            string
	hostName          string
	version           string
	distro            string
	os                string
	osVersion         string
	configFile        string
	options           map[string]any
	configYAML        []byte
	memberState       string
	replsetConfig     map[string]any
	replsetConfigYAML []byte
}

func printStartup(info *startupInfoT) {
	if !info.complete {
		return // nothing here
	}
	startMsg := "Log rotation"
	if info.isStartup {
		startMsg = "Start up"
	}
	fmt.Printf("%s | host: %s | port: %d | dbPath: %s | pid: %d | when: %s UTC\n", startMsg, info.hostName, info.port, info.dbPath, info.processID, info.timeStamp.UTC().Format(time.ANSIC))
	fmt.Printf("Version: %s | Platform: %s | OS: %s | OS Version: %s\n", info.version, info.distro, info.os, info.osVersion)
	fmt.Printf("%s\n", info.configYAML)
	if info.replsetConfig != nil {
		fmt.Printf("Member state: %s\n", info.memberState)
		fmt.Printf("%s\n", info.replsetConfigYAML)
	}
	info.complete = false
	info.isStartup = false
}

func logLine(line []byte, startupInfo *startupInfoT) (*logT, error) {
	lineObj := logJSONT{}
	err := json.Unmarshal(line, &lineObj)
	if err != nil {
		if strings.HasPrefix(string(line), skippingLines) {
			fmt.Printf("Warning: lines skipped in log file! %s\n", string(line))
			return nil, nil
		}
		return nil, fmt.Errorf("error parsing log line for JSON: %v", err)
	}

	timeStamp, err := time.Parse(timeLayout, lineObj.T.Date)
	if err != nil {
		return nil, fmt.Errorf("invalid timestamp: %v", err)
	}
	// fmt.Printf("Time: %#v\n", timeStamp.UTC())
	logMsg := logT{
		timeStamp: timeStamp,
	}
	attr := lineObj.Attr
	if attr != nil && (lineObj.C == "CONTROL" || lineObj.C == "REPL") {
		attr := attr.(map[string]any)
		switch lineObj.MSG {
		case "MongoDB starting":
			startupInfo.isStartup = true
			startupInfo.timeStamp = logMsg.timeStamp
			startupInfo.processID = int(attr["pid"].(float64))
			startupInfo.port = int(attr["port"].(float64))
			startupInfo.hostName = attr["host"].(string)
			startupInfo.dbPath = attr["dbPath"].(string)
		case "Process Details":
			startupInfo.isStartup = false // just a log rotation
			startupInfo.timeStamp = logMsg.timeStamp
			startupInfo.processID, _ = strconv.Atoi(attr["pid"].(string))
			startupInfo.port = int(attr["port"].(float64))
			startupInfo.hostName = attr["host"].(string)
		case "Build Info":
			biattrb := attr["buildInfo"].(map[string]any)
			startupInfo.version = biattrb["version"].(string)
			biattrenv := biattrb["environment"].(map[string]any)
			startupInfo.distro = biattrenv["distmod"].(string)
		case "Operating System":
			osattros := attr["os"].(map[string]any)
			startupInfo.os = osattros["name"].(string)
			startupInfo.osVersion = osattros["version"].(string)
		case "Node is a member of a replica set":
			startupInfo.memberState = attr["memberState"].(string)
			startupInfo.replsetConfig = attr["config"].(map[string]any)
			rsconfigYAML, err := getConfig(startupInfo.replsetConfig)
			if err == nil {
				startupInfo.replsetConfigYAML = rsconfigYAML
			} else {
				startupInfo.replsetConfigYAML = nil
			}
		case "New replica set config in use":
			rsConfig := attr["config"].(map[string]any)
			rsConfigYAML, err := getConfig(rsConfig)
			if err != nil {
				startupInfo.replsetConfigYAML = nil
			}
			fmt.Printf("New replica set config: %s\n%s\n", timeStamp.UTC().Format(time.ANSIC), rsConfigYAML)
		case "Options set by command line":
			opattropts := attr["options"].(map[string]any)
			startupInfo.configFile = opattropts["config"].(string)
			startupInfo.options = opattropts
			configYAML, err := getConfig(opattropts)
			if err == nil {
				startupInfo.configYAML = configYAML
			} else {
				startupInfo.configYAML = nil
			}
			startupInfo.complete = true
		}
	}
	return &logMsg, nil
}

func getConfig(config map[string]any) ([]byte, error) {
	yamlBytes, err := yaml.Marshal(config)
	return yamlBytes, err
}
