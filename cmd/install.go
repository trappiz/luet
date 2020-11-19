// Copyright © 2019 Ettore Di Giacinto <mudler@gentoo.org>
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
package cmd

import (
	"os"
	"path/filepath"

	installer "github.com/mudler/luet/pkg/installer"
	"github.com/mudler/luet/pkg/solver"

	helpers "github.com/mudler/luet/cmd/helpers"
	. "github.com/mudler/luet/pkg/config"
	. "github.com/mudler/luet/pkg/logger"
	pkg "github.com/mudler/luet/pkg/package"

	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:     "install <pkg1> <pkg2> ...",
	Short:   "Install a package",
	Aliases: []string{"i"},
	PreRun: func(cmd *cobra.Command, args []string) {
		LuetCfg.Viper.BindPFlag("system.database_path", cmd.Flags().Lookup("system-dbpath"))
		LuetCfg.Viper.BindPFlag("system.rootfs", cmd.Flags().Lookup("system-target"))
		LuetCfg.Viper.BindPFlag("solver.type", cmd.Flags().Lookup("solver-type"))
		LuetCfg.Viper.BindPFlag("solver.discount", cmd.Flags().Lookup("solver-discount"))
		LuetCfg.Viper.BindPFlag("solver.rate", cmd.Flags().Lookup("solver-rate"))
		LuetCfg.Viper.BindPFlag("solver.max_attempts", cmd.Flags().Lookup("solver-attempts"))
		LuetCfg.Viper.BindPFlag("onlydeps", cmd.Flags().Lookup("onlydeps"))
		LuetCfg.Viper.BindPFlag("nodeps", cmd.Flags().Lookup("nodeps"))
		LuetCfg.Viper.BindPFlag("force", cmd.Flags().Lookup("force"))
	},
	Long: `Install packages in parallel`,
	Run: func(cmd *cobra.Command, args []string) {
		toInstall := pkg.NewPackages()
		var systemDB pkg.PackageDatabase

		for _, a := range args {
			pack, err := helpers.ParsePackageStr(a)
			if err != nil {
				Fatal("Invalid package string ", a, ": ", err.Error())
			}
			toInstall.Put(pack)
		}

		// This shouldn't be necessary, but we need to unmarshal the repositories to a concrete struct, thus we need to port them back to the Repositories type
		repos := installer.Repositories{}
		for _, repo := range LuetCfg.SystemRepositories {
			if !repo.Enable {
				continue
			}
			r := installer.NewSystemRepository(repo)
			repos = append(repos, r)
		}

		stype := LuetCfg.Viper.GetString("solver.type")
		discount := LuetCfg.Viper.GetFloat64("solver.discount")
		rate := LuetCfg.Viper.GetFloat64("solver.rate")
		attempts := LuetCfg.Viper.GetInt("solver.max_attempts")
		force := LuetCfg.Viper.GetBool("force")
		nodeps := LuetCfg.Viper.GetBool("nodeps")
		onlydeps := LuetCfg.Viper.GetBool("onlydeps")
		concurrent, _ := cmd.Flags().GetBool("solver-concurrent")

		LuetCfg.GetSolverOptions().Type = stype
		LuetCfg.GetSolverOptions().LearnRate = float32(rate)
		LuetCfg.GetSolverOptions().Discount = float32(discount)
		LuetCfg.GetSolverOptions().MaxAttempts = attempts

		if concurrent {
			LuetCfg.GetSolverOptions().Implementation = solver.ParallelSimple
		} else {
			LuetCfg.GetSolverOptions().Implementation = solver.SingleCoreSimple
		}

		Debug("Solver", LuetCfg.GetSolverOptions().CompactString())

		// Load config protect configs
		installer.LoadConfigProtectConfs(LuetCfg)

		inst := installer.NewLuetInstaller(installer.LuetInstallerOptions{
			Concurrency:                 LuetCfg.GetGeneral().Concurrency,
			SolverOptions:               *LuetCfg.GetSolverOptions(),
			NoDeps:                      nodeps,
			Force:                       force,
			OnlyDeps:                    onlydeps,
			PreserveSystemEssentialData: true,
		})
		inst.Repositories(repos)

		if LuetCfg.GetSystem().DatabaseEngine == "boltdb" {
			systemDB = pkg.NewBoltDatabase(
				filepath.Join(LuetCfg.GetSystem().GetSystemRepoDatabaseDirPath(), "luet.db"))
		} else {
			systemDB = pkg.NewInMemoryDatabase(true)
		}
		system := &installer.System{Database: systemDB, Target: LuetCfg.GetSystem().Rootfs}
		err := inst.Install(toInstall, system)
		if err != nil {
			Fatal("Error: " + err.Error())
		}
	},
}

func init() {
	path, err := os.Getwd()
	if err != nil {
		Fatal(err)
	}
	installCmd.Flags().String("system-dbpath", path, "System db path")
	installCmd.Flags().String("system-target", path, "System rootpath")
	installCmd.Flags().String("solver-type", "", "Solver strategy ( Defaults none, available: "+AvailableResolvers+" )")
	installCmd.Flags().Float32("solver-rate", 0.7, "Solver learning rate")
	installCmd.Flags().Float32("solver-discount", 1.0, "Solver discount rate")
	installCmd.Flags().Int("solver-attempts", 9000, "Solver maximum attempts")
	installCmd.Flags().Bool("nodeps", false, "Don't consider package dependencies (harmful!)")
	installCmd.Flags().Bool("onlydeps", false, "Consider **only** package dependencies")
	installCmd.Flags().Bool("force", false, "Skip errors and keep going (potentially harmful)")
	installCmd.Flags().Bool("solver-concurrent", false, "Use concurrent solver (experimental)")

	RootCmd.AddCommand(installCmd)
}
