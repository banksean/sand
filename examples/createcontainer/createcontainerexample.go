package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"time"

	ac "github.com/banksean/apple-container"
	"github.com/banksean/apple-container/options"
)

func main() {
	ctx := context.Background()
	fmt.Println("Creating container...")
	id, err := ac.Containers.Create(ctx,
		options.CreateContainer{
			ManagementOptions: options.ManagementOptions{
				Name: "applecontainer-demo",
			},
		},
		"web-test", nil)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	ctr, err := ac.Containers.Inspect(ctx, id)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	ctrJSON, err := json.MarshalIndent(ctr, "", "  ")
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	fmt.Println("Newly created container:")
	fmt.Println(string(ctrJSON))
	fmt.Println("Starting container...")
	id, err = ac.Containers.Start(ctx, options.StartContainer{
		Debug: true,
	}, id)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	ctr, err = ac.Containers.Inspect(ctx, id)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	ctrJSON, err = json.MarshalIndent(ctr, "", "  ")
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	fmt.Println("Newly started container:")
	fmt.Println(string(ctrJSON))

	timeout := 5 * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel() // Ensure the context is canceled to release resources
	logs, wait, err := ac.Containers.Logs(ctx, options.ContainerLogs{
		Boot:   true,
		Follow: true,
	}, id)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	defer logs.Close()

	fmt.Printf("Scanning container logs, with a %v timeout...\n", timeout)
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

	fmt.Println("Stopping container...")
	id, err = ac.Containers.Stop(ctx, options.StopContainer{}, id)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	fmt.Printf("Container %s stopped\n", id)
}
