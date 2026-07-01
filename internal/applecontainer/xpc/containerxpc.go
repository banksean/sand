//go:build darwin && cgo

package xpc

import (
	"context"
	"fmt"
)

func Demo() {
	containers, err := ListContainers()
	if err != nil {
		fmt.Printf("error listing containers over XPC: %v\n", err)
		return
	}
	fmt.Printf("%d containers\n", len(containers))
}

func ListContainers() ([]ContainerSnapshot, error) {
	client, err := NewClient()
	if err != nil {
		return nil, err
	}
	defer client.Close()
	return client.ListContainers(context.Background(), ContainerListFilters{})
}
