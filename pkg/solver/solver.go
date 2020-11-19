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

	"github.com/pkg/errors"

	"github.com/crillab/gophersat/bf"
	pkg "github.com/mudler/luet/pkg/package"
)

type SolverType int

const (
	SingleCoreSimple = 0
	ParallelSimple   = iota
)

// PackageSolver is an interface to a generic package solving algorithm
type PackageSolver interface {
	SetDefinitionDatabase(pkg.PackageDatabase)
	Install(p *pkg.Packages) (PackagesAssertions, error)
	Uninstall(candidate pkg.Package, checkconflicts, full bool) (*pkg.Packages, error)
	ConflictsWithInstalled(p pkg.Package) (bool, error)
	ConflictsWith(p pkg.Package, ls *pkg.Packages) (bool, error)
	Conflicts(pack pkg.Package, lsp *pkg.Packages) (bool, error)

	World() *pkg.Packages
	Upgrade(checkconflicts, full bool) (*pkg.Packages, PackagesAssertions, error)

	UpgradeUniverse(dropremoved bool) (*pkg.Packages, PackagesAssertions, error)
	UninstallUniverse(toremove *pkg.Packages) (*pkg.Packages, error)

	SetResolver(PackageResolver)

	Solve() (PackagesAssertions, error)
}

// Solver is the default solver for luet
type Solver struct {
	DefinitionDatabase pkg.PackageDatabase
	SolverDatabase     pkg.PackageDatabase
	Wanted             *pkg.Packages
	InstalledDatabase  pkg.PackageDatabase

	Resolver PackageResolver
}

type Options struct {
	Type        SolverType
	Concurrency int
}

// NewSolver accepts as argument two lists of packages, the first is the initial set,
// the second represent all the known packages.
func NewSolver(t Options, installed pkg.PackageDatabase, definitiondb pkg.PackageDatabase, solverdb pkg.PackageDatabase) PackageSolver {
	return NewResolver(t, installed, definitiondb, solverdb, &DummyPackageResolver{})
}

// NewResolver accepts as argument two lists of packages, the first is the initial set,
// the second represent all the known packages.
// Using constructors as in the future we foresee warmups for hot-restore solver cache
func NewResolver(t Options, installed pkg.PackageDatabase, definitiondb pkg.PackageDatabase, solverdb pkg.PackageDatabase, re PackageResolver) PackageSolver {
	var s PackageSolver
	switch t.Type {
	case SingleCoreSimple:
		s = &Solver{InstalledDatabase: installed, DefinitionDatabase: definitiondb, SolverDatabase: solverdb, Resolver: re}
	case ParallelSimple:
		s = &Parallel{InstalledDatabase: installed, DefinitionDatabase: definitiondb, ParallelDatabase: solverdb, Resolver: re, Concurrency: t.Concurrency}
	}

	return s
}

// SetDefinitionDatabase is a setter for the definition Database

func (s *Solver) SetDefinitionDatabase(db pkg.PackageDatabase) {
	s.DefinitionDatabase = db
}

// SetResolver is a setter for the unsat resolver backend
func (s *Solver) SetResolver(r PackageResolver) {
	s.Resolver = r
}

func (s *Solver) World() *pkg.Packages {
	return s.DefinitionDatabase.World()
}

func (s *Solver) Installed() *pkg.Packages {
	return s.InstalledDatabase.World()
}

func (s *Solver) noRulesWorld() bool {
	for _, p := range s.World().List {
		if len(p.GetConflicts()) != 0 || len(p.GetRequires()) != 0 {
			return false
		}
	}

	return true
}

func (s *Solver) noRulesInstalled() bool {
	for _, p := range s.Installed().List {
		if len(p.GetConflicts()) != 0 || len(p.GetRequires()) != 0 {
			return false
		}
	}

	return true
}

func (s *Solver) BuildInstalled() (bf.Formula, error) {
	var formulas []bf.Formula
	packages := pkg.NewPackages()
	for _, p := range s.Installed().List {
		packages.Put(p)
		for _, dep := range p.Related(s.DefinitionDatabase.World()).List {
			packages.Put(dep)
		}
	}

	for _, p := range packages.List {
		solvable, err := p.BuildFormula(s.DefinitionDatabase, s.SolverDatabase)
		if err != nil {
			return nil, err
		}
		//f = bf.And(f, solvable)
		formulas = append(formulas, solvable...)

	}
	return bf.And(formulas...), nil

}

// BuildWorld builds the formula which olds the requirements from the package definitions
// which are available (global state)
func (s *Solver) BuildWorld(includeInstalled bool) (bf.Formula, error) {
	var formulas []bf.Formula
	// NOTE: This block should be enabled in case of very old systems with outdated world sets
	if includeInstalled {
		solvable, err := s.BuildInstalled()
		if err != nil {
			return nil, err
		}
		//f = bf.And(f, solvable)
		formulas = append(formulas, solvable)
	}

	for _, p := range s.World().List {

		solvable, err := p.BuildFormula(s.DefinitionDatabase, s.SolverDatabase)
		if err != nil {
			return nil, err
		}
		formulas = append(formulas, solvable...)
	}
	return bf.And(formulas...), nil
}

func (s *Solver) generateFormulas(formulas []bf.Formula, packs *pkg.Packages) ([]bf.Formula, error) {
	for _, p := range packs.List {
		for _, dep := range p.Related(s.DefinitionDatabase.World()).List {

			solvable, err := dep.BuildFormula(s.DefinitionDatabase, s.SolverDatabase)
			if err != nil {
				return nil, err
			}
			formulas = append(formulas, solvable...)
		}
	}
	return formulas, nil
}

// BuildWorld builds the formula which olds the requirements from the package definitions
// which are available (global state)
func (s *Solver) BuildPartialWorld(includeInstalled bool) (bf.Formula, error) {
	var formulas []bf.Formula
	// NOTE: This block shouldf be enabled in case of very old systems with outdated world sets
	if includeInstalled {
		solvable, err := s.BuildInstalled()
		if err != nil {
			return nil, err
		}
		//f = bf.And(f, solvable)
		formulas = append(formulas, solvable)
	}

	var err error
	formulas, err = s.generateFormulas(formulas, s.Wanted)

	if err != nil {
		return nil, err
	}

	if len(formulas) != 0 {
		return bf.And(formulas...), nil
	}
	return bf.True, nil

}

func (s *Solver) getList(db pkg.PackageDatabase, lsp *pkg.Packages) (*pkg.Packages, error) {
	ls := pkg.NewPackages()

	for _, pp := range lsp.List {
		cp, err := db.FindPackage(pp)
		if err != nil {
			packages, err := db.FindPackageVersions(pp)
			// Expand, and relax search - if not found pick the same one
			if err != nil || packages.Len() == 0 {
				cp = pp
			} else {
				cp = packages.Best(nil)
			}
		}
		ls.Put(cp)
	}
	return ls, nil
}

// Conflicts acts like ConflictsWith, but uses package's reverse dependencies to
// determine if it conflicts with the given set
func (s *Solver) Conflicts(pack pkg.Package, lsp *pkg.Packages) (bool, error) {
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

	return !revdeps.Empty(), revdepsErr
}

// ConflictsWith return true if a package is part of the requirement set of a list of package
// return false otherwise (and thus it is NOT relevant to the given list)
func (s *Solver) ConflictsWith(pack pkg.Package, lsp *pkg.Packages) (bool, error) {
	p, err := s.DefinitionDatabase.FindPackage(pack)
	if err != nil {
		p = pack //Relax search, otherwise we cannot compute solutions for packages not in definitions

		//	return false, errors.Wrap(err, "Package not found in definition db")
	}

	ls, err := s.getList(s.DefinitionDatabase, lsp)
	if err != nil {
		return false, errors.Wrap(err, "Package not found in definition db")
	}

	var formulas []bf.Formula

	if s.noRulesWorld() {
		return false, nil
	}

	encodedP, err := p.Encode(s.SolverDatabase)
	if err != nil {
		return false, err
	}
	P := bf.Var(encodedP)

	r, err := s.BuildWorld(false)
	if err != nil {
		return false, err
	}
	formulas = append(formulas, bf.And(bf.Not(P), r))

	for _, i := range ls.List {
		if i.Matches(p) {
			continue
		}
		// XXX: Skip check on any of its requires ?  ( Drop to avoid removing system packages when selecting an uninstall)
		// if i.RequiresContains(p) {
		// 	fmt.Println("Requires found")
		// 	continue
		// }

		encodedI, err := i.Encode(s.SolverDatabase)
		if err != nil {
			return false, err
		}
		I := bf.Var(encodedI)
		formulas = append(formulas, bf.And(I, r))
	}
	model := bf.Solve(bf.And(formulas...))
	if model == nil {
		return true, nil
	}

	return false, nil

}

func (s *Solver) ConflictsWithInstalled(p pkg.Package) (bool, error) {
	return s.ConflictsWith(p, s.Installed())
}

// UninstallUniverse takes a list of candidate package and return a list of packages that would be removed
// in order to purge the candidate. Uses the solver to check constraints and nothing else
//
// It can be compared to the counterpart Uninstall as this method acts like a uninstall --full
// it removes all the packages and its deps. taking also in consideration other packages that might have
// revdeps
func (s *Solver) UninstallUniverse(toremove *pkg.Packages) (*pkg.Packages, error) {

	if s.noRulesInstalled() {
		return s.getList(s.InstalledDatabase, toremove)
	}
	markedForRemoval := pkg.NewPackages()

	// resolve to packages from the db
	toRemove, err := s.getList(s.InstalledDatabase, toremove)
	if err != nil {
		return markedForRemoval, errors.Wrap(err, "Package not found in definition db")
	}

	var formulas []bf.Formula
	r, err := s.BuildInstalled()
	if err != nil {
		return markedForRemoval, errors.Wrap(err, "Package not found in definition db")
	}

	// SAT encode the clauses against the world
	for _, p := range toRemove.Unique().List {
		encodedP, err := p.Encode(s.InstalledDatabase)
		if err != nil {
			return markedForRemoval, errors.Wrap(err, "Package not found in definition db")
		}
		P := bf.Var(encodedP)
		formulas = append(formulas, bf.And(bf.Not(P), r))
	}

	model := bf.Solve(bf.And(formulas...))
	if model == nil {
		return markedForRemoval, errors.New("Failed finding a solution")
	}
	assertion, err := DecodeModel(model, s.InstalledDatabase)
	if err != nil {
		return markedForRemoval, errors.Wrap(err, "while decoding model from solution")
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
func (s *Solver) UpgradeUniverse(dropremoved bool) (*pkg.Packages, PackagesAssertions, error) {
	// we first figure out which aren't up-to-date
	// which has to be removed
	// and which needs to be upgraded
	notUptodate := pkg.NewPackages()
	removed := pkg.NewPackages()
	toUpgrade := pkg.NewPackages()
	markedForRemoval := pkg.NewPackages()

	// TODO: this is memory expensive, we need to optimize this
	universe := pkg.NewInMemoryDatabase(false)
	s.DefinitionDatabase.Clone(universe)
	s.Installed().Clone(universe)

	// Grab all the installed ones, see if they are eligible for update
	for _, p := range s.Installed().List {
		available, err := universe.FindPackageVersions(p)
		if err != nil {
			removed.Put(p)
		}
		if available.Len() == 0 {
			continue
		}

		bestmatch := available.Best(nil)
		// Found a better version available
		if !bestmatch.Matches(p) {
			notUptodate.Put(p)
			toUpgrade.Put(bestmatch)
		}
	}

	fmt.Println("Found", toUpgrade.Unique())

	var formulas []bf.Formula

	// Build constraints for the whole defdb
	//	r, err := s.BuildWorld(true)
	//	if err != nil {
	//		return nil, nil, errors.Wrap(err, "couldn't build world constraints")
	//	}

	// Treat removed packages from universe as marked for deletion
	if dropremoved {
		notUptodate.Put(removed.List...)
	}

	var formulas2 []bf.Formula

	var err error
	//formulas2 = append(formulas2, solvable)
	formulas2, err = s.generateFormulas(formulas2, notUptodate.Unique())
	formulas2, err = s.generateFormulas(formulas2, toUpgrade.Unique())

	r := bf.And(formulas2...)

	for _, p := range notUptodate.Unique().List {

		encodedP, err := p.Encode(universe)
		if err != nil {
			return markedForRemoval, nil, errors.Wrap(err, "couldn't encode package")
		}
		P := bf.Var(encodedP)
		formulas = append(formulas, bf.And(bf.Not(P), r))
	}

	for _, p := range toUpgrade.Unique().List {
		encodedP, err := p.Encode(universe)
		if err != nil {
			return markedForRemoval, nil, errors.Wrap(err, "couldn't encode package")
		}
		P := bf.Var(encodedP)
		formulas = append(formulas, bf.And(P, r))
	}

	if len(formulas) == 0 {
		return pkg.NewPackages(), PackagesAssertions{}, nil
	}
	model := bf.Solve(bf.And(formulas...))
	if model == nil {
		return markedForRemoval, nil, errors.New("Failed finding a solution")
	}

	assertion, err := DecodeModel(model, universe)
	if err != nil {
		return markedForRemoval, nil, errors.Wrap(err, "while decoding model from solution")
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

func (s *Solver) Upgrade(checkconflicts, full bool) (*pkg.Packages, PackagesAssertions, error) {

	// First get candidates that needs to be upgraded..

	toUninstall := pkg.NewPackages()
	toInstall := pkg.NewPackages()

	// we do this in memory so we take into account of provides
	universe := pkg.NewInMemoryDatabase(false)
	s.DefinitionDatabase.Clone(universe)

	installedcopy := pkg.NewInMemoryDatabase(false)

	for _, p := range s.InstalledDatabase.World().List {
		installedcopy.CreatePackage(p)
		packages, err := universe.FindPackageVersions(p)
		if err == nil && !packages.Empty() {
			best := packages.Best(nil)
			if !best.Matches(p) {
				toUninstall.Put(p)
				toInstall.Put(best)
			}
		}
	}

	s2 := NewSolver(Options{Type: SingleCoreSimple}, installedcopy, s.DefinitionDatabase, pkg.NewInMemoryDatabase(false))
	s2.SetResolver(s.Resolver)
	if !full {
		ass := PackagesAssertions{}
		for _, i := range toInstall.List {
			ass = append(ass, PackageAssert{Package: i.(*pkg.DefaultPackage), Value: true})
		}
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
	if toInstall.Empty() {
		return toUninstall, PackagesAssertions{}, nil
	}
	r, e := s2.Install(toInstall)
	return toUninstall, r, e
	// To that tree, ask to install the versions that should be upgraded, and try to solve
	// Return the solution
}

// Uninstall takes a candidate package and return a list of packages that would be removed
// in order to purge the candidate. Returns error if unsat.
// TODO:XXX Make it work for a list of pkgs instead
func (s *Solver) Uninstall(c pkg.Package, checkconflicts, full bool) (*pkg.Packages, error) {
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
			return res, err
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

	s2 := NewSolver(Options{Type: SingleCoreSimple}, pkg.NewInMemoryDatabase(false), s.DefinitionDatabase, pkg.NewInMemoryDatabase(false))
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

// BuildFormula builds the main solving formula that is evaluated by the sat solver.
func (s *Solver) BuildFormula() (bf.Formula, error) {
	var formulas []bf.Formula
	r, err := s.BuildPartialWorld(false)
	if err != nil {
		return nil, err
	}

	for _, wanted := range s.Wanted.List {
		encodedW, err := wanted.Encode(s.SolverDatabase)
		if err != nil {
			return nil, err
		}
		W := bf.Var(encodedW)
		//	allW = append(allW, W)
		installedWorld := s.Installed()
		//TODO:Optimize
		if installedWorld.Empty() {
			formulas = append(formulas, W) //bf.And(bf.True, W))
			continue
		}

		installedWorld.Each(func(installed pkg.Package) error {
			encodedI, err := installed.Encode(s.SolverDatabase)
			if err != nil {
				return err
			}
			I := bf.Var(encodedI)
			formulas = append(formulas, bf.And(W, I))
			return nil
		})
	}

	formulas = append(formulas, r)
	return bf.And(formulas...), nil
}

func (s *Solver) solve(f bf.Formula) (map[string]bool, bf.Formula, error) {
	model := bf.Solve(f)
	if model == nil {
		return model, f, errors.New("Unsolvable")
	}

	return model, f, nil
}

// Solve builds the formula given the current state and returns package assertions
func (s *Solver) Solve() (PackagesAssertions, error) {
	var model map[string]bool
	var err error

	f, err := s.BuildFormula()

	if err != nil {
		return nil, err
	}

	model, _, err = s.solve(f)
	if err != nil && s.Resolver != nil {
		fmt.Println(err)
		return s.Resolver.Solve(f, s)
	}

	if err != nil {
		return nil, err
	}

	return DecodeModel(model, s.SolverDatabase)
}

// Install given a list of packages, returns package assertions to indicate the packages that must be installed in the system in order
// to statisfy all the constraints
func (s *Solver) Install(c *pkg.Packages) (PackagesAssertions, error) {

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
