package main

import (
	"bufio"
	"errors"
	"fmt"
	"github.com/fatih/color"
	"github.com/sqweek/dialog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

//The generic structure used for proxies throughout the code
type Proxy struct {
	IP   string
	Port string
	User string
	Pass string
}

//data type used in testProxies() and handleProxyResult()
type ProxyTestResult struct {
	ProxyUsed  Proxy
	Speed      time.Duration
	StatusCode int
	Success    bool
}

//return the ip:port or ip:port:user:pass formatted proxy string
func (p *Proxy) rawString() string {

	raw := p.IP + ":" + p.Port

	if p.User != "" && p.Pass != "" {
		raw = raw + ":" + p.User + ":" + p.Pass
	}

	return raw
}

//returns the string required after declaring the protocol to connect to the proxy
func (p *Proxy) conString() string {

	raw := p.IP + ":" + p.Port

	if p.User != "" && p.Pass != "" {
		raw = p.User + ":" + p.Pass + "@" + raw
	}

	return raw
}

//the function that executes the testing of the proxies and communicates results with handleProxyResult()
func testProxy(proxy Proxy, endpoint string, c chan ProxyTestResult) {

	//create proxy url and add it to the transport
	proxyURL, err := url.Parse(proxy.conString())
	clientTransport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}

	//create client used to send request
	myClient := &http.Client{
		Transport: clientTransport,
		Timeout:   10 * time.Second,
	}

	req, err := http.NewRequest("GET", endpoint, nil)

	//add headers to avoid issues with sites sending error codes for default golang user agent
	req.Header.Add("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/79.0.3945.117 Safari/537.36")
	req.Header.Add("Accept", "*/*")

	if err == nil {

		success := true
		statusCode := -1

		start := time.Now()

		resp, err := myClient.Do(req)

		end := time.Now().Sub(start)

		//handle request response
		if err != nil {
			success = false
		} else {
			statusCode = resp.StatusCode
		}

		//send result to HandleProxyResult() through channel
		c <- ProxyTestResult{proxy, end, statusCode, success}
	}
}

//loads proxies into an array of type Proxy from specified file path
func loadProxies(filePath string) ([]Proxy, error) {

	file, err := os.Open(filePath)

	if err != nil {
		return nil, err
	}

	defer file.Close()

	var proxies []Proxy

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {

		proxy, err := stringToProxy(scanner.Text())

		if err == nil {
			proxies = append(proxies, proxy)
		}
	}

	return proxies, nil
}

//converts a string to the defined data type Proxy
func stringToProxy(line string) (Proxy, error) {

	parts := strings.Split(line, ":")

	if len(parts) == 2 { //ip:port format
		return Proxy{parts[0], parts[1], "", ""}, nil

	} else if len(parts) == 4 { //ip:port:user:pass format
		return Proxy{parts[0], parts[1], parts[2], parts[3]}, nil

	} else { //unknown format, error is returned
		return Proxy{"", "", "", ""}, errors.New("Error parsing proxy")
	}
}

//receives ProxyTestResults from the channel and outputs them to the screen
func handleProxyResult(c chan ProxyTestResult, numOfProxies int, goodProxies *[]string, badProxies *[]string) {

	//create color outputs
	success := color.New(color.FgHiGreen)
	failed := color.New(color.FgHiRed)
	warn := color.New(color.FgHiYellow)

	for i := 0; i < numOfProxies; i++ {

		result := <-c

		fmt.Println("Results for", result.ProxyUsed.rawString())

		if result.Success && result.StatusCode >= 200 && result.StatusCode < 400 { //proxy returned a success status code
			success.Print("Status: OK (", result.StatusCode, ") | Speed: ")
			success.Println(result.Speed)
			*goodProxies = append(*goodProxies, result.ProxyUsed.rawString())

		} else if result.StatusCode == -1 { //error was encountered while testing
			failed.Println("Status: BAD (-1) | Speed: -")
			*badProxies = append(*badProxies, result.ProxyUsed.rawString())

		} else { //proxy is working but endpoint returned a non-success status code
			warn.Print("Status: PROXY WORKING BUT POSSIBLE BAN OR SERVER ERROR (", result.StatusCode, ") | Speed: ")
			warn.Println(result.Speed)
			*goodProxies = append(*goodProxies, result.ProxyUsed.rawString())
		}
	}
	close(c)
}

func writeArrayToFile(arr []string, fileName string) error {

	f, err := os.OpenFile(fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)

	defer f.Close()

	if err != nil {
		return err
	}

	//iterate through array, write to file, check for error
	for _, s := range arr {

		_, err := f.Write([]byte(s + "\n"))

		if err != nil {
			return err
		}
	}

	return nil
}

func main() {

	scanner := bufio.NewScanner(os.Stdin)

	//create the channel used for concurrent proxy testing
	//channel communicates between testProxy() and handleProxyResult()
	//channel transports type ProxyTestResult
	resultChannel := make(chan ProxyTestResult)

	//create the waitgroup used to ensure all proxies are tested
	var WG sync.WaitGroup

	//init waitgroup to 2 task (testProxies() and handleProxyResults()
	WG.Add(1)

	//obtain the endpoint to test the proxies on
	color.Cyan("Enter the url you would like to test the proxies on (eg: https://kith.com): ")
	scanner.Scan()
	endpoint := scanner.Text()

	//non-thorough validation check to avoid reuqest errors
	if !strings.HasPrefix(endpoint, "https://") {
		color.Red("Invalid URL detected. Must be of format https://...")
		scanner.Scan()
		os.Exit(-1)
	}

	//prompt for and obtain the file name containing the proxies
	color.Yellow("Select the file containing the proxies.\nPlease note, the proxies must be in the format ip:port or ip:port:user:pass.\nPress enter to continue...")
	scanner.Scan()

	filePath, err := dialog.File().Filter("Text File", "txt").Title("Select the proxy file").Load()

	//check if operation was canceled
	if err != nil {
		os.Exit(-1)
	}

	//load proxies from entered file and check for errors
	proxies, err := loadProxies(filePath)

	if err != nil {
		color.Red("Error occured while loading proxies. Terminating.")
		color.Red(err.Error())
		scanner.Scan()
		os.Exit(-1)
	}

	//create arrays to store proxy strings of working/non-working proxies
	var goodProxies []string
	var badProxies []string

	//anon funcs below are used for better control over the waitgroup

	go func() {
		handleProxyResult(resultChannel, len(proxies), &goodProxies, &badProxies)
		WG.Done()
	}()

	//interate through proxy list and test all of them
	for _, proxy := range proxies {
		go testProxy(proxy, endpoint, resultChannel)
	}

	//here the waitgroup is used to prevent the main function from ending before the
	WG.Wait()

	//write results to respective files
	err = writeArrayToFile(goodProxies, "working.txt")

	if err != nil {
		color.Red("Failed to write working proxies to \"working.txt\"")
	} else {
		color.Green("Wrote working proxies to \"working.txt\"")
	}

	err = writeArrayToFile(badProxies, "failed.txt")

	if err != nil {
		color.Red("Failed to write bad proxies to \"failed.txt\"")
	} else {
		color.Green("Wrote failed proxies to \"failed.txt\"")
	}

	//finished
	color.Cyan("----- FINISHED -----")
	scanner.Scan()
}
