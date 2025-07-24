package service

import (
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
)

// CategoryService provides business logic for categories
type CategoryService struct {
	repo *repository.CategoryRepository
}

func NewCategoryService(repo *repository.CategoryRepository) *CategoryService {
	return &CategoryService{repo: repo}
}

func (s *CategoryService) Create(category *models.Category) (*models.Category, error) {
	return s.repo.Create(category)
}

func (s *CategoryService) GetAll() ([]models.Category, error) {
	return s.repo.GetAll()
}

func (s *CategoryService) GetByID(id uint) (*models.Category, error) {
	return s.repo.GetByID(id)
}

func (s *CategoryService) Update(id uint, category *models.Category) (*models.Category, error) {
	return s.repo.Update(id, category)
}

func (s *CategoryService) Delete(id uint) error {
	return s.repo.Delete(id)
}
