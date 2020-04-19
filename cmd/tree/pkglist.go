// Copyright © 2020 Ettore Di Giacinto <mudler@gentoo.org>
//                  Daniele Rondina <geaaru@sabayonlinux.org>
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

package cmd_tree

import (
	"fmt"
	"sort"

	//. "github.com/mudler/luet/pkg/config"
	"github.com/ghodss/yaml"
	. "github.com/mudler/luet/pkg/config"
	helpers "github.com/mudler/luet/pkg/helpers"
	. "github.com/mudler/luet/pkg/logger"
	pkg "github.com/mudler/luet/pkg/package"
	tree "github.com/mudler/luet/pkg/tree"

	"github.com/spf13/cobra"
)

type PackageResult struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Version  string `json:"version"`
}

type Results struct {
	Packages []PackageResult `json:"packages"`
}

func pkgDetail(pkg pkg.Package) string {
	ans := fmt.Sprintf(`
  @@ Package: %s/%s-%s
     Description: %s
     License:     %s`,
		pkg.GetCategory(), pkg.GetName(), pkg.GetVersion(),
		pkg.GetDescription(), pkg.GetLicense())

	for idx, u := range pkg.GetURI() {
		if idx == 0 {
			ans += fmt.Sprintf("     URLs:        %s", u)
		} else {
			ans += fmt.Sprintf("                  %s", u)
		}
	}

	return ans
}

func NewTreePkglistCommand() *cobra.Command {
	var excludes []string
	var matches []string

	var ans = &cobra.Command{
		Use:   "pkglist [OPTIONS]",
		Short: "List of the packages found in tree.",
		Args:  cobra.OnlyValidArgs,
		PreRun: func(cmd *cobra.Command, args []string) {
			t, _ := cmd.Flags().GetString("tree")
			if t == "" {
				Fatal("Mandatory tree param missing.")
			}
		},
		Run: func(cmd *cobra.Command, args []string) {
			var results Results

			treePath, _ := cmd.Flags().GetString("tree")
			verbose, _ := cmd.Flags().GetBool("verbose")
			buildtime, _ := cmd.Flags().GetBool("buildtime")
			full, _ := cmd.Flags().GetBool("full")
			out, _ := cmd.Flags().GetString("output")
			if out != "terminal" {
				LuetCfg.GetLogging().SetLogLevel("error")
			}

			var reciper tree.Builder
			if buildtime {
				reciper = tree.NewCompilerRecipe(pkg.NewInMemoryDatabase(false))
			} else {
				reciper = tree.NewInstallerRecipe(pkg.NewInMemoryDatabase(false))
			}

			err := reciper.Load(treePath)
			if err != nil {
				Fatal("Error on load tree ", err)
			}

			regExcludes, err := helpers.CreateRegexArray(excludes)
			if err != nil {
				Fatal(err.Error())
			}
			regMatches, err := helpers.CreateRegexArray(matches)
			if err != nil {
				Fatal(err.Error())
			}

			plist := make([]string, 0)
			for _, p := range reciper.GetDatabase().World() {
				pkgstr := ""
				addPkg := true
				if full {
					pkgstr = pkgDetail(p)
				} else if verbose {
					pkgstr = fmt.Sprintf("%s/%s-%s", p.GetCategory(), p.GetName(), p.GetVersion())
				} else {
					pkgstr = fmt.Sprintf("%s/%s", p.GetCategory(), p.GetName())
				}

				if len(matches) > 0 {
					matched := false
					for _, rgx := range regMatches {
						if rgx.MatchString(pkgstr) {
							matched = true
							break
						}
					}
					if !matched {
						addPkg = false
					}
				}

				if len(excludes) > 0 && addPkg {
					for _, rgx := range regExcludes {
						if rgx.MatchString(pkgstr) {
							addPkg = false
							break
						}
					}
				}

				if addPkg {
					plist = append(plist, pkgstr)
					results.Packages = append(results.Packages, PackageResult{
						Name:     p.GetName(),
						Version:  p.GetVersion(),
						Category: p.GetCategory(),
					})
				}
			}

			y, err := yaml.Marshal(results)
			if err != nil {
				fmt.Printf("err: %v\n", err)
				return
			}
			switch out {
			case "yaml":
				fmt.Println(string(y))
			case "json":
				j2, err := yaml.YAMLToJSON(y)
				if err != nil {
					fmt.Printf("err: %v\n", err)
					return
				}
				fmt.Println(string(j2))
			default:
				sort.Strings(plist)
				for _, p := range plist {
					fmt.Println(p)
				}
			}

		},
	}

	ans.Flags().BoolP("buildtime", "b", false, "Build time match")
	ans.Flags().StringP("output", "o", "terminal", "Output format ( Defaults: terminal, available: json,yaml )")

	ans.Flags().BoolP("verbose", "v", false, "Add package version")
	ans.Flags().BoolP("full", "f", false, "Show package detail")
	ans.Flags().StringP("tree", "t", "", "Path of the tree to use.")
	ans.Flags().StringSliceVarP(&matches, "matches", "m", []string{},
		"Include only matched packages from list. (Use string as regex).")
	ans.Flags().StringSliceVarP(&excludes, "exclude", "e", []string{},
		"Exclude matched packages from list. (Use string as regex).")

	return ans
}
