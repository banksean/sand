package main

import (
	"context"
	"fmt"

	ac "github.com/banksean/apple-container"
)

func main() {
	ctx := context.Background()

	images, err := ac.Images.List(ctx)
	if err != nil {
		fmt.Println("Error listing images:", err)
	}
	for _, image := range images {
		fmt.Printf("image: %v\n", image)
	}

	containers, err := ac.Containers.List(ctx)
	if err != nil {
		fmt.Println("Error listing containers:", err)
	}

	for _, container := range containers {
		fmt.Printf("container: %v\n", container)
	}
}
