/*
 * Minio Cloud Storage, (C) 2015 Minio, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/minio/cli"
	"github.com/minio/minio-xl/pkg/minhttp"
	"github.com/minio/minio-xl/pkg/probe"
)

var serverCmd = cli.Command{
	Name:   "server",
	Usage:  "Start Minio cloud storage server.",
	Action: serverMain,
	CustomHelpTemplate: `NAME:
  minio {{.Name}} - {{.Usage}}

USAGE:
  minio {{.Name}} [OPTION VALUE] PATH

  OPTION = expiry        VALUE = NN[h|m|s] [DEFAULT=Unlimited]
  OPTION = min-free-disk VALUE = NN% [DEFAULT: 10%]

EXAMPLES:
  1. Start minio server on Linux.
      $ minio {{.Name}} /home/shared

  2. Start minio server on Windows.
      $ minio {{.Name}} C:\MyShare

  3. Start minio server bound to a specific IP:PORT, when you have multiple network interfaces.
      $ minio --address 192.168.1.101:9000 {{.Name}} /home/shared

  4. Start minio server with minimum free disk threshold to 5%
      $ minio {{.Name}} min-free-disk 5% /home/shared/Pictures

  5. Start minio server with minimum free disk threshold to 15% with auto expiration set to 1h
      $ minio {{.Name}} min-free-disk 15% expiry 1h /home/shared/Documents

`,
}

// cloudServerConfig - http server config
type cloudServerConfig struct {
	/// HTTP server options
	Address   string // Address:Port listening
	AccessLog bool   // Enable access log handler
	Anonymous bool   // No signature turn off

	/// FS options
	Path        string        // Path to export for cloud storage
	MinFreeDisk int64         // Minimum free disk space for filesystem
	Expiry      time.Duration // Set auto expiry for filesystem

	// TLS service
	TLS      bool   // TLS on when certs are specified
	CertFile string // Domain certificate
	KeyFile  string // Domain key

	/// Advanced HTTP server options
	RateLimit int // Ratelimited server of incoming connections
}

// configureAPIServer configure a new server instance
func configureAPIServer(conf cloudServerConfig) (*http.Server, *probe.Error) {
	// Minio server config
	apiServer := &http.Server{
		Addr:           conf.Address,
		Handler:        getCloudStorageAPIHandler(getNewCloudStorageAPI(conf)),
		MaxHeaderBytes: 1 << 20,
	}

	if conf.TLS {
		var err error
		apiServer.TLSConfig = &tls.Config{}
		apiServer.TLSConfig.Certificates = make([]tls.Certificate, 1)
		apiServer.TLSConfig.Certificates[0], err = tls.LoadX509KeyPair(conf.CertFile, conf.KeyFile)
		if err != nil {
			return nil, probe.NewError(err)
		}
	}

	host, port, err := net.SplitHostPort(conf.Address)
	if err != nil {
		return nil, probe.NewError(err)
	}

	var hosts []string
	switch {
	case host != "":
		hosts = append(hosts, host)
	default:
		addrs, err := net.InterfaceAddrs()
		if err != nil {
			return nil, probe.NewError(err)
		}
		for _, addr := range addrs {
			if addr.Network() == "ip+net" {
				host := strings.Split(addr.String(), "/")[0]
				if ip := net.ParseIP(host); ip.To4() != nil {
					hosts = append(hosts, host)
				}
			}
		}
	}

	Println("Starting minio server:")
	for _, host := range hosts {
		if conf.TLS {
			Printf("Listening on https://%s:%s\n", host, port)
		} else {
			Printf("Listening on http://%s:%s\n", host, port)
		}
	}
	return apiServer, nil
}

// startServer starts an s3 compatible cloud storage server
func startServer(conf cloudServerConfig) *probe.Error {
	apiServer, err := configureAPIServer(conf)
	if err != nil {
		return err.Trace()
	}
	rateLimit := conf.RateLimit
	if err := minhttp.ListenAndServeLimited(rateLimit, apiServer); err != nil {
		return err.Trace()
	}
	return nil
}

// parse input string with percent to int64
func parsePercentToInt(s string, bitSize int) (int64, *probe.Error) {
	i := strings.Index(s, "%")
	if i < 0 {
		// no percentage string found try to parse the whole string anyways
		p, err := strconv.ParseInt(s, 10, bitSize)
		if err != nil {
			return 0, probe.NewError(err)
		}
		return p, nil
	}
	p, err := strconv.ParseInt(s[:i], 10, bitSize)
	if err != nil {
		return 0, probe.NewError(err)
	}
	return p, nil
}
func setLogger(conf *configV2) *probe.Error {
	if conf.IsMongoLoggingEnabled() {
		err := log2Mongo(conf.MongoLogger.Addr, conf.MongoLogger.DB, conf.MongoLogger.Collection)
		if err != nil {
			return err.Trace()
		}
	}
	if conf.IsSysloggingEnabled() {
		err := log2Syslog(conf.SyslogLogger.Network, conf.SyslogLogger.Addr)
		if err != nil {
			return err.Trace()
		}
	}
	if conf.IsFileLoggingEnabled() {
		err := log2File(conf.FileLogger.Filename)
		if err != nil {
			return err.Trace()
		}
	}
	return nil
}

// Generates config if it doesn't exist, otherwise returns back the saved ones.
func getConfig() (*configV2, *probe.Error) {
	if err := createConfigPath(); err != nil {
		return nil, err.Trace()
	}
	config, err := loadConfigV2()
	if err != nil {
		if os.IsNotExist(err.ToGoError()) {
			// Initialize new config, since config file doesn't exist yet
			config := &configV2{}
			config.Version = "2"
			config.Credentials.AccessKeyID = string(mustGenerateAccessKeyID())
			config.Credentials.SecretAccessKey = string(mustGenerateSecretAccessKey())
			if err := saveConfig(config); err != nil {
				return nil, err.Trace()
			}
			return config, nil
		}
		return nil, err.Trace()
	}
	return config, nil
}

type accessKeys struct {
	*configV2
}

func (a accessKeys) String() string {
	magenta := color.New(color.FgMagenta, color.Bold).SprintFunc()
	white := color.New(color.FgWhite, color.Bold).SprintfFunc()
	return fmt.Sprint(magenta("AccessKey: ") + white(a.Credentials.AccessKeyID) + "  " + magenta("SecretKey: ") + white(a.Credentials.SecretAccessKey))
}

// JSON - json formatted output
func (a accessKeys) JSON() string {
	b, err := json.Marshal(a)
	errorIf(probe.NewError(err), "Unable to marshal json", nil)
	return string(b)
}

// initServer initialize server
func initServer() *probe.Error {
	conf, err := getConfig()
	if err != nil {
		return err.Trace()
	}
	if err := setLogger(conf); err != nil {
		return err.Trace()
	}
	if conf != nil {
		if globalJSONFlag {
			Println(accessKeys{conf}.JSON())
		} else {
			Println()
			Println(accessKeys{conf})
		}
	}
	if !globalJSONFlag {
		Println("\nTo configure Minio Client.")
		if runtime.GOOS == "windows" {
			Println("\n\tDownload https://dl.minio.io:9000/updates/2015/Oct/" + runtime.GOOS + "-" + runtime.GOARCH + "/mc.exe")
			Println("\t$ mc.exe config host add localhost:9000 " + conf.Credentials.AccessKeyID + " " + conf.Credentials.SecretAccessKey)
			Println("\t$ mc.exe mb localhost/photobucket")
			Println("\t$ mc.exe cp C:\\Photos... localhost/photobucket")
		} else {
			Println("\n\t$ wget https://dl.minio.io:9000/updates/2015/Oct/" + runtime.GOOS + "-" + runtime.GOARCH + "/mc")
			Println("\t$ chmod 755 mc")
			Println("\t$ ./mc config host add localhost:9000 " + conf.Credentials.AccessKeyID + " " + conf.Credentials.SecretAccessKey)
			Println("\t$ ./mc mb localhost/photobucket")
			Println("\t$ ./mc cp ~/Photos... localhost/photobucket")
		}
		Println()
	}
	return nil
}

func checkServerSyntax(c *cli.Context) {
	if !c.Args().Present() || c.Args().First() == "help" {
		cli.ShowCommandHelpAndExit(c, "server", 1)
	}
	if len(c.Args()) > 5 {
		fatalIf(probe.NewError(errInvalidArgument), "Unnecessary arguments passed. Please refer ‘mc server help’", nil)
	}
	path := strings.TrimSpace(c.Args().Last())
	if path == "" {
		fatalIf(probe.NewError(errInvalidArgument), "Path argument cannot be empty.", nil)
	}
}

func serverMain(c *cli.Context) {
	checkServerSyntax(c)

	perr := initServer()
	fatalIf(perr.Trace(), "Failed to read config for minio.", nil)

	certFile := c.GlobalString("cert")
	keyFile := c.GlobalString("key")
	if (certFile != "" && keyFile == "") || (certFile == "" && keyFile != "") {
		fatalIf(probe.NewError(errInvalidArgument), "Both certificate and key are required to enable https.", nil)
	}

	var minFreeDisk int64
	minFreeDiskSet := false
	// Default
	minFreeDisk = 10

	var expiration time.Duration
	expirationSet := false

	args := c.Args()
	for len(args) >= 2 {
		switch args.First() {
		case "min-free-disk":
			if minFreeDiskSet {
				fatalIf(probe.NewError(errInvalidArgument), "Minimum free disk should be set only once.", nil)
			}
			args = args.Tail()
			var err *probe.Error
			minFreeDisk, err = parsePercentToInt(args.First(), 64)
			fatalIf(err.Trace(args.First()), "Invalid minium free disk size "+args.First()+" passed.", nil)
			args = args.Tail()
			minFreeDiskSet = true
		case "expiry":
			if expirationSet {
				fatalIf(probe.NewError(errInvalidArgument), "Expiration should be set only once.", nil)
			}
			args = args.Tail()
			var err error
			expiration, err = time.ParseDuration(args.First())
			fatalIf(probe.NewError(err), "Invalid expiration time "+args.First()+" passed.", nil)
			args = args.Tail()
			expirationSet = true
		default:
			cli.ShowCommandHelpAndExit(c, "server", 1) // last argument is exit code
		}
	}

	path := strings.TrimSpace(c.Args().Last())
	// Last argument is always path
	if _, err := os.Stat(path); err != nil {
		fatalIf(probe.NewError(err), "Unable to validate the path", nil)
	}
	tls := (certFile != "" && keyFile != "")
	apiServerConfig := cloudServerConfig{
		Address:     c.GlobalString("address"),
		AccessLog:   c.GlobalBool("enable-accesslog"),
		Anonymous:   c.GlobalBool("anonymous"),
		Path:        path,
		MinFreeDisk: minFreeDisk,
		Expiry:      expiration,
		TLS:         tls,
		CertFile:    certFile,
		KeyFile:     keyFile,
		RateLimit:   c.GlobalInt("ratelimit"),
	}
	perr = startServer(apiServerConfig)
	errorIf(perr.Trace(), "Failed to start the minio server.", nil)
}
