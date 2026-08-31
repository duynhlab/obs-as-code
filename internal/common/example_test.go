package common_test

import (
	"fmt"

	"github.com/duynhlab/obs-as-code/internal/common"
	"github.com/duynhlab/obs-as-code/internal/profile"
)

// ExampleNewDashboard shows the two lines every board file starts with.
func ExampleNewDashboard() {
	p := profile.Cluster()

	board, err := common.NewDashboard(p, "my-board", "My Board", "http").Build()
	if err != nil {
		panic(err)
	}

	fmt.Println(*board.Uid)
	fmt.Println(board.Tags)
	fmt.Println(*board.Refresh)
	fmt.Println(board.Templating.List[0].Name)

	// Output:
	// my-board
	// [obs-as-code http]
	// 1m
	// ds
}
