// Package dto holds the Admin Panel's request and response shapes. These are the
// only values that cross the panel's HTTP boundary: the entity never leaves the
// service layer, so a password hash cannot be serialized by accident (golden
// rule 6).
package dto

import "time"

// CreateAdminRequest is an owner adding a colleague. The binding tags are the
// transport's own gate; the service re-checks every rule, because a DTO that is
// only validated at the edge is a rule the next transport forgets.
type CreateAdminRequest struct {
	Email    string `json:"email" binding:"required" example:"support@example.test"`
	Role     string `json:"role" binding:"required" example:"support"`
	Password string `json:"password" binding:"required" example:"kata-sandi-yang-cukup-panjang"`
}

// AdminResponse is one admin as the panel reads them. There is deliberately no
// field for the password hash or its cost (ADR 0024 §1).
type AdminResponse struct {
	ID        uint      `json:"id" example:"2"`
	Email     string    `json:"email" example:"support@example.test"`
	Role      string    `json:"role" example:"support"`
	IsActive  bool      `json:"is_active" example:"true"`
	CreatedAt time.Time `json:"created_at"`
}
