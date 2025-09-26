package main

import (
	"bufio"
	"context"
	"fmt"
	"time"

	applecontainer "github.com/banksean/apple-container"
	"github.com/banksean/apple-container/options"
)

func main() {
	ctx := context.Background()
	fmt.Println("Starting container system...")
	res, err := applecontainer.System.Start(ctx, options.SystemStart{Debug: true})
	if err != nil {
		fmt.Printf("error: %v\n", err)
	}
	fmt.Printf("container system start output:\n%s\n", res)

	fmt.Println("Starting container system...")
	res, err = applecontainer.System.Status(ctx, options.SystemStatus{Debug: true})
	if err != nil {
		fmt.Printf("error: %v\n", err)
	}
	fmt.Printf("container system status output:\n%s\n", res)

	timeout := 5 * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel() // Ensure the context is canceled to release resources
	logs, wait, err := applecontainer.System.Logs(ctx, options.SystemLogs{
		Follow: true,
	})
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	defer logs.Close()

	fmt.Printf("Scanning system logs, with a %v timeout...\n", timeout)
	go func() {
		logScanner := bufio.NewScanner(logs)
		for logScanner.Scan() {
			fmt.Printf("Log line: %s\n", logScanner.Text())
		}
		if logScanner.Err() != nil {
			fmt.Printf("logScanner error: %v\n", err)
		}
	}()

	if err := wait(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			fmt.Printf("%v timeout expired\n", timeout)
		} else {
			fmt.Printf("wait error: %v\n", err)
			return
		}
	}

	fmt.Println("Stopping container system...")
	res, err = applecontainer.System.Stop(ctx, options.SystemStop{Debug: true})
	if err != nil {
		fmt.Printf("error: %v\n", err)
	}
	fmt.Printf("container system stop output:\n%s\n", res)
}
