package student

import "fmt"

type Student struct {
	ID   int64   `json:"id"`
	Name string  `json:"name"`
	GPA  float64 `json:"gpa"`
}

func New(id int64, name string) (Student, error) {
	if name == "" {
		return Student{}, fmt.Errorf("name is required")
	}
	return Student{ID: id, Name: name}, nil
}
