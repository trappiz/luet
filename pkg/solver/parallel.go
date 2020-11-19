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

package solver

import (

	//. "github.com/mudler/luet/pkg/logger"
	"fmt"
	"sync"

	"github.com/pkg/errors"

	"github.com/crillab/gophersat/bf"
	pkg "github.com/mudler/luet/pkg/package"
)

// Parallel is the default Parallel for luet
type Parallel struct {
	Concurrency        int
	DefinitionDatabase pkg.PackageDatabase
	ParallelDatabase   pkg.PackageDatabase
	Wanted             *pkg.Packages
	InstalledDatabase  pkg.PackageDatabase

	Resolver PackageResolver
}

func (s *Parallel) SetDefinitionDatabase(db pkg.PackageDatabase) {
	s.DefinitionDatabase = db
}

// SetReSolver is a setter for the unsat ReSolver backend
func (s *Parallel) SetResolver(r PackageResolver) {
	s.Resolver = r
}

func (s *Parallel) World() *pkg.Packages {
	return s.DefinitionDatabase.World()
}

func (s *Parallel) Installed() *pkg.Packages {

	return s.InstalledDatabase.World()
}

func (s *Parallel) noRulesWorld() bool {
	for _, p := range s.World().List {
		if len(p.GetConflicts()) != 0 || len(p.GetRequires()) != 0 {
			return false
		}
	}

	return true
}

func (s *Parallel) noRulesInstalled() bool {
	for _, p := range s.Installed().List {
		if len(p.GetConflicts()) != 0 || len(p.GetRequires()) != 0 {
			return false
		}
	}

	return true
}

func (s *Parallel) buildParallelFormula(formulas []bf.Formula, packages *pkg.Packages) ([]bf.Formula, error) {
	var wg = new(sync.WaitGroup)
	var wg2 = new(sync.WaitGroup)

	all := make(chan pkg.Package)
	results := make(chan bf.Formula, 1)
	for i := 0; i < s.Concurrency; i++ {
		wg.Add(1)
		go func(wg *sync.WaitGroup, c <-chan pkg.Package) {
			defer wg.Done()
			for p := range c {
				solvable, err := p.BuildFormula(s.DefinitionDatabase, s.ParallelDatabase)
				if err != nil {
					panic(err)
				}
				for _, s := range solvable {
					results <- s
				}
			}
		}(wg, all)
	}
	wg2.Add(1)
	go func() {
		defer wg2.Done()
		for t := range results {
			formulas = append(formulas, t)
		}
	}()

	for _, p := range packages.List {
		all <- p
	}

	close(all)
	wg.Wait()
	close(results)
	wg2.Wait()

	return formulas, nil
}

func (s *Parallel) BuildInstalled() (bf.Formula, error) {
	var formulas []bf.Formula
	var err error
	packages := pkg.NewPackages()

	for _, p := range s.Installed().List {
		packages.Put(p)
		for _, dep := range p.Related(s.DefinitionDatabase.World()).List {
			packages.Put(dep)
		}

	}

	formulas, err = s.buildParallelFormula(formulas, packages)
	if err != nil {
		return nil, err
	}
	if len(formulas) != 0 {
		return bf.And(formulas...), nil
	}
	return bf.True, nil
}

// BuildWorld builds the formula which olds the requirements from the package definitions
// which are available (global state)
func (s *Parallel) BuildWorld(includeInstalled bool) (bf.Formula, error) {
	var formulas []bf.Formula
	var err error
	// NOTE: This block should be enabled in case of very old systems with outdated world sets
	if includeInstalled {
		solvable, err := s.BuildInstalled()
		if err != nil {
			return nil, err
		}
		//f = bf.And(f, solvable)
		formulas = append(formulas, solvable)
	}

	formulas, err = s.buildParallelFormula(formulas, s.World())
	if err != nil {
		return nil, err
	}
	if len(formulas) != 0 {
		return bf.And(formulas...), nil
	}
	return bf.True, nil
}

// BuildWorld builds the formula which olds the requirements from the package definitions
// which are available (global state)
func (s *Parallel) BuildPartialWorld(includeInstalled bool) (bf.Formula, error) {
	var formulas []bf.Formula
	var err error
	// NOTE: This block should be enabled in case of very old systems with outdated world sets
	if includeInstalled {
		solvable, err := s.BuildInstalled()
		if err != nil {
			return nil, err
		}
		//f = bf.And(f, solvable)
		formulas = append(formulas, solvable)
	}

	var wg = new(sync.WaitGroup)
	var wg2 = new(sync.WaitGroup)

	world := s.DefinitionDatabase.World()
	packages := pkg.NewPackages()

	all := make(chan pkg.Package)
	results := make(chan pkg.Package, 1)
	for i := 0; i < s.Concurrency; i++ {
		wg.Add(1)
		go func(wg *sync.WaitGroup, c <-chan pkg.Package) {
			defer wg.Done()
			for p := range c {
				for _, dep := range p.Related(world).List {
					results <- dep
				}

			}
		}(wg, all)
	}
	wg2.Add(1)
	go func() {
		defer wg2.Done()
		for t := range results {
			packages.Put(t)
		}
	}()

	for _, p := range s.Wanted.List {
		all <- p
	}

	close(all)
	wg.Wait()
	close(results)
	wg2.Wait()

	formulas, err = s.buildParallelFormula(formulas, packages)
	if err != nil {
		return nil, err
	}
	if len(formulas) != 0 {
		return bf.And(formulas...), nil
	}
	return bf.True, nil

	//return s.buildParallelFormula(formulas, s.World())
}

func (s *Parallel) getList(db pkg.PackageDatabase, lsp *pkg.Packages) (*pkg.Packages, error) {
	ls := &pkg.Packages{}
	var wg = new(sync.WaitGroup)
	var wg2 = new(sync.WaitGroup)

	all := make(chan pkg.Package)
	results := make(chan pkg.Package, 1)
	for i := 0; i < s.Concurrency; i++ {
		wg.Add(1)
		go func(wg *sync.WaitGroup, c <-chan pkg.Package) {
			defer wg.Done()
			for p := range c {
				cp, err := db.FindPackage(p)
				if err != nil {
					packages, err := db.FindPackageVersions(p)
					// Expand, and relax search - if not found pick the same one
					if err != nil || packages.Len() == 0 {
						cp = p
					} else {
						cp = packages.Best(nil)
					}
				}
				results <- cp
			}
		}(wg, all)
	}

	wg2.Add(1)
	go func(wg *sync.WaitGroup) {
		defer wg2.Done()
		for t := range results {
			ls.Put(t)
		}
	}(wg)

	for _, pp := range lsp.List {
		all <- pp
	}

	close(all)
	wg.Wait()
	close(results)
	wg2.Wait()

	return ls, nil
}

// Conflicts acts like ConflictsWith, but uses package's reverse dependencies to
// determine if it conflicts with the given set
func (s *Parallel) Conflicts(pack pkg.Package, lsp *pkg.Packages) (bool, error) {
	p, err := s.DefinitionDatabase.FindPackage(pack)
	if err != nil {
		p = pack
	}

	ls, err := s.getList(s.DefinitionDatabase, lsp)
	if err != nil {
		return false, errors.Wrap(err, "Package not found in definition db")
	}

	if s.noRulesWorld() {
		return false, nil
	}

	visited := make(map[string]interface{})
	revdeps := p.ExpandedRevdeps(ls, visited)

	var revdepsErr error
	for _, r := range revdeps.List {
		if revdepsErr == nil {
			revdepsErr = errors.New("")
		}
		revdepsErr = errors.New(fmt.Sprintf("%s\n%s", revdepsErr.Error(), r.HumanReadableString()))
	}

	return revdeps.Len() != 0, revdepsErr
}

// ConflictsWith return true if a package is part of the requirement set of a list of package
// return false otherwise (and thus it is NOT relevant to the given list)
func (s *Parallel) ConflictsWith(pack pkg.Package, lsp *pkg.Packages) (bool, error) {
	p, err := s.DefinitionDatabase.FindPackage(pack)
	if err != nil {
		p = pack //Relax search, otherwise we cannot compute solutions for packages not in definitions
	}

	ls, err := s.getList(s.DefinitionDatabase, lsp)
	if err != nil {
		return false, errors.Wrap(err, "Package not found in definition db")
	}

	var formulas []bf.Formula

	if s.noRulesWorld() {
		return false, nil
	}

	encodedP, err := p.Encode(s.ParallelDatabase)
	if err != nil {
		return false, err
	}
	P := bf.Var(encodedP)

	r, err := s.BuildWorld(false)
	if err != nil {
		return false, err
	}
	formulas = append(formulas, bf.And(bf.Not(P), r))

	var wg = new(sync.WaitGroup)
	var wg2 = new(sync.WaitGroup)

	all := make(chan pkg.Package)
	results := make(chan bf.Formula, 1)
	for i := 0; i < s.Concurrency; i++ {
		wg.Add(1)
		go func(wg *sync.WaitGroup, c <-chan pkg.Package) {
			defer wg.Done()
			for i := range c {
				if i.Matches(p) {
					continue
				}

				// XXX: Skip check on any of its requires ?  ( Drop to avoid removing system packages when selecting an uninstall)
				// if i.RequiresContains(p) {
				// 	fmt.Println("Requires found")
				// 	continue
				// }

				encodedI, err := i.Encode(s.ParallelDatabase)
				if err != nil {
					panic(err)
				}
				I := bf.Var(encodedI)

				results <- bf.And(I, r)
			}
		}(wg, all)
	}

	wg2.Add(1)
	go func() {
		defer wg2.Done()
		for t := range results {
			formulas = append(formulas, t)
		}
	}()

	for _, p := range ls.List {
		all <- p
	}

	close(all)
	wg.Wait()
	close(results)
	wg2.Wait()

	model := bf.Solve(bf.And(formulas...))
	if model == nil {
		return true, nil
	}

	return false, nil

}

func (s *Parallel) ConflictsWithInstalled(p pkg.Package) (bool, error) {
	return s.ConflictsWith(p, s.Installed())
}

// UninstallUniverse takes a list of candidate package and return a list of packages that would be removed
// in order to purge the candidate. Uses the Parallel to check constraints and nothing else
//
// It can be compared to the counterpart Uninstall as this method acts like a uninstall --full
// it removes all the packages and its deps. taking also in consideration other packages that might have
// revdeps
func (s *Parallel) UninstallUniverse(toremove *pkg.Packages) (*pkg.Packages, error) {

	if s.noRulesInstalled() {
		return s.getList(s.InstalledDatabase, toremove)
	}

	// resolve to packages from the db
	toRemove, err := s.getList(s.InstalledDatabase, toremove)
	if err != nil {
		return nil, errors.Wrap(err, "Package not found in definition db")
	}

	var formulas []bf.Formula
	r, err := s.BuildInstalled()
	if err != nil {
		return nil, errors.Wrap(err, "Package not found in definition db")
	}

	// SAT encode the clauses against the world
	for _, p := range toRemove.Unique().List {
		encodedP, err := p.Encode(s.InstalledDatabase)
		if err != nil {
			return nil, errors.Wrap(err, "Package not found in definition db")
		}
		P := bf.Var(encodedP)
		formulas = append(formulas, bf.And(bf.Not(P), r))
	}

	markedForRemoval := pkg.NewPackages()
	model := bf.Solve(bf.And(formulas...))
	if model == nil {
		return nil, errors.New("Failed finding a solution")
	}
	assertion, err := DecodeModel(model, s.InstalledDatabase)
	if err != nil {
		return nil, errors.Wrap(err, "while decoding model from solution")
	}
	for _, a := range assertion {
		if !a.Value {
			if p, err := s.InstalledDatabase.FindPackage(a.Package); err == nil {
				markedForRemoval.Put(p)
			}

		}
	}
	return markedForRemoval, nil
}

// UpgradeUniverse mark packages for removal and returns a solution. It considers
// the Universe db as authoritative
// See also on the subject: https://arxiv.org/pdf/1007.1021.pdf
func (s *Parallel) UpgradeUniverse(dropremoved bool) (*pkg.Packages, PackagesAssertions, error) {
	var formulas []bf.Formula
	// we first figure out which aren't up-to-date
	// which has to be removed
	// and which needs to be upgraded
	removed := pkg.NewPackages()

	// TODO: this is memory expensive, we need to optimize this
	universe := pkg.NewInMemoryDatabase(false)

	s.DefinitionDatabase.World().Clone(universe)
	s.Installed().Clone(universe)
	// Build constraints for the whole defdb
	r, err := s.BuildWorld(true)
	if err != nil {
		return removed, nil, errors.Wrap(err, "couldn't build world constraints")
	}
	var wg = new(sync.WaitGroup)
	var wg2 = new(sync.WaitGroup)

	all := make(chan pkg.Package)
	results := make(chan bf.Formula, 1)
	for i := 0; i < s.Concurrency; i++ {
		wg.Add(1)
		go func(wg *sync.WaitGroup, c <-chan pkg.Package) {
			defer wg.Done()
			for p := range c {
				available, err := universe.FindPackageVersions(p)
				if err != nil {
					removed.Put(p) /// FIXME: Racy
				}
				if available.Len() == 0 {
					continue
				}

				bestmatch := available.Best(nil)
				// Found a better version available
				if !bestmatch.Matches(p) {
					encodedP, _ := p.Encode(universe)
					P := bf.Var(encodedP)
					results <- bf.And(bf.Not(P), r)
					encodedP, _ = bestmatch.Encode(universe)
					P = bf.Var(encodedP)
					results <- bf.And(P, r)
				}
			}
		}(wg, all)
	}

	wg2.Add(1)
	go func() {
		defer wg2.Done()
		for t := range results {
			formulas = append(formulas, t)
		}
	}()

	// Grab all the installed ones, see if they are eligible for update
	for _, p := range s.Installed().List {
		all <- p
	}

	close(all)
	wg.Wait()
	close(results)
	wg2.Wait()

	// Treat removed packages from universe as marked for deletion
	if dropremoved {

		// SAT encode the clauses against the world
		for _, p := range removed.List {
			encodedP, err := p.Encode(universe)
			if err != nil {
				return nil, nil, errors.Wrap(err, "couldn't encode package")
			}
			P := bf.Var(encodedP)
			formulas = append(formulas, bf.And(bf.Not(P), r))
		}
	}

	markedForRemoval := pkg.NewPackages()
	if len(formulas) == 0 {
		return nil, PackagesAssertions{}, nil
	}
	model := bf.Solve(bf.And(formulas...))
	if model == nil {
		return nil, nil, errors.New("Failed finding a solution")
	}

	assertion, err := DecodeModel(model, universe)
	if err != nil {
		return nil, nil, errors.Wrap(err, "while decoding model from solution")
	}
	for _, a := range assertion {
		if !a.Value {
			if p, err := s.InstalledDatabase.FindPackage(a.Package); err == nil {
				markedForRemoval.Put(p)
			}

		}

	}
	return markedForRemoval, assertion, nil
}

// Upgrade compute upgrades of the package against the world definition.
// It accepts two boolean indicating if it has to check for conflicts or try to attempt a full upgrade
func (s *Parallel) Upgrade(checkconflicts, full bool) (*pkg.Packages, PackagesAssertions, error) {

	// First get candidates that needs to be upgraded..

	toUninstall := pkg.NewPackages()
	toInstall := pkg.NewPackages()

	// we do this in memory so we take into account of provides
	universe := pkg.NewInMemoryDatabase(false)
	s.DefinitionDatabase.World().Clone(universe)

	installedcopy := pkg.NewInMemoryDatabase(false)

	var wg = new(sync.WaitGroup)
	var wg2 = new(sync.WaitGroup)

	all := make(chan pkg.Package)
	results := make(chan []pkg.Package, 1)
	for i := 0; i < s.Concurrency; i++ {
		wg.Add(1)
		go func(wg *sync.WaitGroup, c <-chan pkg.Package) {
			defer wg.Done()
			for p := range c {
				installedcopy.CreatePackage(p)
				packages, err := universe.FindPackageVersions(p)
				if err == nil && packages.Len() != 0 {
					best := packages.Best(nil)
					if !best.Matches(p) {
						results <- []pkg.Package{p, best}
					}
				}
			}
		}(wg, all)
	}

	wg2.Add(1)
	go func() {
		defer wg2.Done()
		for t := range results {
			toUninstall.Put(t[0])
			toInstall.Put(t[1])
		}
	}()

	for _, p := range s.InstalledDatabase.World().List {
		all <- p
	}

	close(all)
	wg.Wait()
	close(results)
	wg2.Wait()

	s2 := &Parallel{Concurrency: s.Concurrency, InstalledDatabase: installedcopy, DefinitionDatabase: s.DefinitionDatabase, ParallelDatabase: pkg.NewInMemoryDatabase(false)}
	s2.SetResolver(s.Resolver)
	if !full {
		ass := PackagesAssertions{}
		toInstall.Each(func(i pkg.Package) error {
			ass = append(ass, PackageAssert{Package: i.(*pkg.DefaultPackage), Value: true})
			return nil
		})
	}

	// Then try to uninstall the versions in the system, and store that tree
	for _, p := range toUninstall.List {
		r, err := s.Uninstall(p, checkconflicts, false)
		if err != nil {
			return toUninstall, nil, errors.Wrap(err, "Could not compute upgrade - couldn't uninstall selected candidate "+p.GetFingerPrint())
		}
		for _, z := range r.List {
			err = installedcopy.RemovePackage(z)
			if err != nil {
				return toUninstall, nil, errors.Wrap(err, "Could not compute upgrade - couldn't remove copy of package targetted for removal")
			}
		}
	}
	if toInstall.Len() == 0 {
		return toUninstall, PackagesAssertions{}, nil
	}
	r, e := s2.Install(toInstall)
	return toUninstall, r, e
	// To that tree, ask to install the versions that should be upgraded, and try to solve
	// Return the solution

}

// Uninstall takes a candidate package and return a list of packages that would be removed
// in order to purge the candidate. Returns error if unsat.
func (s *Parallel) Uninstall(c pkg.Package, checkconflicts, full bool) (*pkg.Packages, error) {
	res := pkg.NewPackages()
	candidate, err := s.InstalledDatabase.FindPackage(c)
	if err != nil {

		//	return nil, errors.Wrap(err, "Couldn't find required package in db definition")
		packages, err := s.InstalledDatabase.FindPackageVersions(c)
		//	Info("Expanded", packages, err)
		if err != nil || packages.Empty() {
			candidate = c
		} else {
			candidate = packages.Best(nil)
		}
		//Relax search, otherwise we cannot compute solutions for packages not in definitions
		//	return nil, errors.Wrap(err, "Package not found between installed")
	}

	// We are asked to not perform a full uninstall (checking all the possible requires that could
	// be removed). Let's only check if we can remove the selected package
	if !full && checkconflicts {
		if conflicts, err := s.Conflicts(candidate, s.Installed()); conflicts {
			return nil, err
		} else {
			return pkg.NewPackages(candidate), nil
		}
	}

	// Build a fake "Installed" - Candidate and its requires tree
	InstalledMinusCandidate := s.Installed().Search(func(p pkg.Package) bool {
		if !p.Matches(candidate) {
			contains, _ := candidate.RequiresContains(s.Installed(), p)
			return !contains
		}
		return false
	})

	s2 := &Parallel{Concurrency: s.Concurrency, InstalledDatabase: pkg.NewInMemoryDatabase(false), DefinitionDatabase: s.DefinitionDatabase, ParallelDatabase: pkg.NewInMemoryDatabase(false)}
	s2.SetResolver(s.Resolver)
	// Get the requirements to install the candidate
	asserts, err := s2.Install(pkg.NewPackages(candidate))
	if err != nil {
		return res, err
	}
	for _, a := range asserts {
		if a.Value {
			if !checkconflicts {
				res.Put(a.Package)
				continue
			}

			c, err := s.ConflictsWithInstalled(a.Package)
			if err != nil {
				return res, err
			}

			// If doesn't conflict with installed we just consider it for removal and look for the next one
			if !c {
				res.Put(a.Package)
				continue
			}

			// If does conflicts, give it another chance by checking conflicts if in case we didn't installed our candidate and all the required packages in the system
			c, err = s.ConflictsWith(a.Package, InstalledMinusCandidate)
			if err != nil {
				return res, err
			}
			if !c {
				res.Put(a.Package)
			}

		}

	}

	return res, nil
}

// BuildFormula builds the main solving formula that is evaluated by the sat Parallel.
func (s *Parallel) BuildFormula() (bf.Formula, error) {
	var formulas []bf.Formula

	r, err := s.BuildPartialWorld(false)
	if err != nil {
		return nil, err
	}

	var wg = new(sync.WaitGroup)
	var wg2 = new(sync.WaitGroup)

	all := make(chan pkg.Package)
	results := make(chan bf.Formula, 1)
	for i := 0; i < s.Concurrency; i++ {
		wg.Add(1)
		go func(wg *sync.WaitGroup, c <-chan pkg.Package) {
			defer wg.Done()
			for wanted := range c {
				encodedW, err := wanted.Encode(s.ParallelDatabase)
				if err != nil {
					panic(err)
				}
				W := bf.Var(encodedW)
				installedWorld := s.Installed()
				//TODO:Optimize
				if installedWorld.Len() == 0 {
					results <- W
					continue
				}

				for _, installed := range installedWorld.List {
					encodedI, err := installed.Encode(s.ParallelDatabase)
					if err != nil {
						panic(err)
					}
					I := bf.Var(encodedI)
					results <- bf.And(W, I)
				}
			}
		}(wg, all)
	}
	wg2.Add(1)
	go func() {
		defer wg2.Done()
		for t := range results {
			formulas = append(formulas, t)
		}
	}()

	for _, wanted := range s.Wanted.List {
		all <- wanted
	}

	close(all)
	wg.Wait()
	close(results)
	wg2.Wait()

	formulas = append(formulas, r)

	return bf.And(formulas...), nil
}

func (s *Parallel) solve(f bf.Formula) (map[string]bool, bf.Formula, error) {
	model := bf.Solve(f)
	if model == nil {
		return model, f, errors.New("Unsolvable")
	}

	return model, f, nil
}

// Solve builds the formula given the current state and returns package assertions
func (s *Parallel) Solve() (PackagesAssertions, error) {
	var model map[string]bool
	var err error

	f, err := s.BuildFormula()

	if err != nil {
		return nil, err
	}

	model, _, err = s.solve(f)
	if err != nil && s.Resolver != nil {
		return s.Resolver.Solve(f, s)
	}

	if err != nil {
		return nil, err
	}

	return DecodeModel(model, s.ParallelDatabase)
}

// Install given a list of packages, returns package assertions to indicate the packages that must be installed in the system in order
// to statisfy all the constraints
func (s *Parallel) Install(c *pkg.Packages) (PackagesAssertions, error) {

	coll, err := s.getList(s.DefinitionDatabase, c)
	if err != nil {
		return nil, errors.Wrap(err, "Packages not found in definition db")
	}

	s.Wanted = coll

	if s.noRulesWorld() {
		var ass PackagesAssertions
		add := func(p pkg.Package) error {
			ass = append(ass, PackageAssert{Package: p.(*pkg.DefaultPackage), Value: true})
			return nil
		}

		s.Installed().Each(add)
		s.Wanted.Each(add)
		return ass, nil
	}

	return s.Solve()
}
