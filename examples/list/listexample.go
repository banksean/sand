package main

import (
	"fmt"

	applecontainer "github.com/banksean/apple-container"
)

func main() {
	images, err := applecontainer.ListAllImages()
	if err != nil {
		fmt.Println("Error listing images:", err)
	}
	for _, image := range images {
		fmt.Printf("image: %v\n", image)
	}
	containers, err := applecontainer.ListAllContainers()
	if err != nil {
		fmt.Println("Error listing containers:", err)
	}
	for _, container := range containers {
		fmt.Printf("container: %v\n", container)
	}
}
