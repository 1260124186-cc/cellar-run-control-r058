package service

import (
	"sort"

	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/domain"
)

func (s *Service) CreateVessel(input CreateVesselInput) (VesselView, error) {
	now := s.clock.Now()
	vessel := domain.Vessel{
		Code:      domain.NormalizeVesselCode(input.Code),
		CapacityL: input.CapacityL,
		State:     domain.VesselAvailable,
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := vessel.Validate(); err != nil {
		return VesselView{}, err
	}
	id, err := generateEntityID("vessel")
	if err != nil {
		return VesselView{}, domain.NewError(domain.CodeStorage, err.Error())
	}
	vessel.ID = id

	var view VesselView
	err = s.store.Execute(func(snapshot *domain.Snapshot) error {
		if _, exists := snapshot.VesselIndex[vessel.Code]; exists {
			return domain.NewError(domain.CodeVesselConflict,
				"a vessel with the same code already exists")
		}
		snapshot.Vessels[vessel.ID] = vessel
		snapshot.VesselIndex[vessel.Code] = vessel.ID
		view = vesselView(*snapshot, vessel)
		return nil
	})
	if err != nil {
		return VesselView{}, err
	}
	return view, nil
}

func (s *Service) GetVessel(id string) (VesselView, error) {
	var view VesselView
	err := s.store.View(func(snapshot domain.Snapshot) error {
		vessel, ok := snapshot.Vessels[id]
		if !ok {
			return domain.NewError(domain.CodeVesselNotFound, "vessel was not found")
		}
		view = vesselView(snapshot, vessel)
		return nil
	})
	if err != nil {
		return VesselView{}, err
	}
	return view, nil
}

func (s *Service) ListVessels() ([]VesselView, error) {
	var vessels []VesselView
	err := s.store.View(func(snapshot domain.Snapshot) error {
		vessels = make([]VesselView, 0, len(snapshot.Vessels))
		for _, vessel := range snapshot.Vessels {
			vessels = append(vessels, vesselView(snapshot, vessel))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(vessels, func(i, j int) bool {
		if vessels[i].Code == vessels[j].Code {
			return vessels[i].ID < vessels[j].ID
		}
		return vessels[i].Code < vessels[j].Code
	})
	return vessels, nil
}

func (s *Service) FinishCleaning(
	id string,
	input FinishCleaningInput,
) (VesselView, error) {
	var view VesselView
	err := s.store.Execute(func(snapshot *domain.Snapshot) error {
		vessel, ok := snapshot.Vessels[id]
		if !ok {
			return domain.NewError(domain.CodeVesselNotFound, "vessel was not found")
		}
		if err := s.checkExpected(vessel.Version, input.ExpectedVersion); err != nil {
			return err
		}
		if vessel.ActiveRunID == nil {
			return domain.NewError(domain.CodeVesselState,
				"cleaning vessel has no associated run")
		}
		run, ok := snapshot.Runs[*vessel.ActiveRunID]
		if !ok {
			return domain.NewError(domain.CodeRunNotFound,
				"associated run was not found")
		}
		if run.State != domain.RunCompleted && run.State != domain.RunAborted {
			return domain.NewError(domain.CodeRunState,
				"associated run is still open")
		}
		if err := vessel.FinishCleaning(s.clock.Now()); err != nil {
			return err
		}
		snapshot.Vessels[id] = vessel
		view = vesselView(*snapshot, vessel)
		return nil
	})
	if err != nil {
		return VesselView{}, err
	}
	return view, nil
}

func (s *Service) RetireVessel(id string, input VersionInput) (VesselView, error) {
	var view VesselView
	err := s.store.Execute(func(snapshot *domain.Snapshot) error {
		vessel, ok := snapshot.Vessels[id]
		if !ok {
			return domain.NewError(domain.CodeVesselNotFound, "vessel was not found")
		}
		if err := s.checkExpected(vessel.Version, input.ExpectedVersion); err != nil {
			return err
		}
		if err := vessel.Retire(s.clock.Now()); err != nil {
			return err
		}
		snapshot.Vessels[id] = vessel
		view = vesselView(*snapshot, vessel)
		return nil
	})
	if err != nil {
		return VesselView{}, err
	}
	return view, nil
}
