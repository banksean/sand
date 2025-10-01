package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
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
	fmt.Printf("Starting container %s...\n", id)
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
	fmt.Printf("Newly started container %s:\n", id)
	fmt.Println(string(ctrJSON))

	timeout := 5 * time.Second
	ctxLogs, cancel := context.WithTimeout(ctx, timeout)
	defer cancel() // Ensure the context is canceled to release resources
	logs, waitLogs, err := ac.Containers.Logs(ctxLogs, options.ContainerLogs{
		Boot:   true,
		Follow: true,
	}, id)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	defer logs.Close()

	fmt.Printf("Scanning container %s logs, with a %v timeout...\n", id, timeout)
	go func() {
		logScanner := bufio.NewScanner(logs)
		for logScanner.Scan() {
			fmt.Printf("Log line: %s\n", logScanner.Text())
		}
		if logScanner.Err() != nil {
			fmt.Printf("logScanner error: %v\n", err)
		}
	}()

	if err := waitLogs(); err != nil {
		if ctxLogs.Err() == context.DeadlineExceeded {
			fmt.Printf("%v timeout expired\n", timeout)
		} else {
			fmt.Printf("wait error: %v\n", err)
			return
		}
	}

	fmt.Printf("Executinging `ls` in container %s:\n", id)

	ctxExec, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	waitExec, err := ac.Containers.ExecStream(ctxExec, options.ExecContainer{}, id, "ls", os.Environ(), os.Stdin, os.Stdout, os.Stderr)

	if err := waitExec(); err != nil {
		if ctxExec.Err() == context.DeadlineExceeded {
			fmt.Printf("%v timeout expired\n", timeout)
		} else {
			fmt.Printf("wait error: %v\n", err)
			return
		}

	}

	fmt.Printf("Stopping container %s...\n", id)
	id, err = ac.Containers.Stop(ctx, options.StopContainer{}, id)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	fmt.Printf("Container %s stopped\n", id)

	fmt.Printf("Deleting container %s...\n", id)
	id, err = ac.Containers.Delete(ctx, options.DeleteContainer{}, id)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	fmt.Printf("Container %s deleted\n", id)

}
