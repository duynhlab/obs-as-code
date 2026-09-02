package common_test

import (
	"fmt"

	"github.com/duynhlab/obs-as-code/internal/common"
	"github.com/duynhlab/obs-as-code/internal/profile"
)

// ExampleNewDashboard shows the two lines every board file starts with.
func ExampleNewDashboard() {
	p := profile.Cluster()

	board, err := common.NewDashboard(p, meta("my-board", "My Board"), "http").Build()
	if err != nil {
		panic(err)
	}

	fmt.Println(board.Title)
	fmt.Println(board.Tags)
	fmt.Println(board.TimeSettings.AutoRefresh)
	fmt.Println(board.Variables[0].DatasourceVariableKind.Spec.Name)

	// Output:
	// My Board
	// [obs-as-code owner:platform http]
	// 1m
	// ds
}
