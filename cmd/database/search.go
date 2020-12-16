// Copyright © 2020 Ettore Di Giacinto <mudler@gentoo.org>
//
// This program is free software; you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation; either version 2 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License along
// with this program; if not, see <http://www.gnu.org/licenses/>.

package cmd_database

import (
	"fmt"

	helpers "github.com/mudler/luet/cmd/helpers"
	. "github.com/mudler/luet/pkg/logger"
	pkg "github.com/mudler/luet/pkg/package"
	tree "github.com/mudler/luet/pkg/tree"

	. "github.com/mudler/luet/pkg/config"

	"github.com/spf13/cobra"
)

func NewDatabaseSearchCommand() *cobra.Command {
	var treePaths []string

	var ans = &cobra.Command{
		Use:   "search <pkg>",
		Short: "Search a package in the system DB",
		Args:  cobra.OnlyValidArgs,
		PreRun: func(cmd *cobra.Command, args []string) {
			LuetCfg.Viper.BindPFlag("system.database_path", cmd.Flags().Lookup("system-dbpath"))
			LuetCfg.Viper.BindPFlag("system.rootfs", cmd.Flags().Lookup("system-target"))

		},
		Run: func(cmd *cobra.Command, args []string) {

			//		var systemDB pkg.PackageDatabase
			pack, err := helpers.ParsePackageStr(args[0])

			reciper := (tree.NewInstallerRecipe(pkg.NewInMemoryDatabase(false))).(*tree.InstallerRecipe)

			for _, treePath := range treePaths {
				Info(fmt.Sprintf("Loading :deciduous_tree: %s...", treePath))
				err = reciper.Load(treePath)
				if err != nil {
					Fatal("Error on load tree ", err)
				}
			}

			fmt.Println("PACK ", pack)
			deps, err := reciper.GetDatabase().FindPackages(pack)
			if err != nil {
				Fatal("Error on find package ", err)
			}

			fmt.Println("DEPS ", deps)

			//		res, err := reciper.GetDatabase().FindPackage(pack)
			//		fmt.Println("RES ", res)
		},
	}

	ans.Flags().StringSliceVarP(&treePaths, "tree", "t", []string{},
		"Path of the tree to use.")

	return ans
}
