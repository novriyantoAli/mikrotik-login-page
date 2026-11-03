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

// LoginRequest is an operator signing in with the panel's own credential.
type LoginRequest struct {
	Email    string `json:"email" binding:"required" example:"owner@example.test"`
	Password string `json:"password" binding:"required" example:"kata-sandi-yang-cukup-panjang"`
}

// SessionResponse is the identity behind the admin cookie. It is the body of both
// login and the session read, so the panel has one shape to parse.
//
// AdminID is never serialized: the panel already knows who it is from the email
// and role, while the server needs the id to attribute an audit row without a
// second lookup. A `json:"-"` field is the smallest way to hand both callers the
// same value.
type SessionResponse struct {
	AdminID   uint      `json:"-"`
	Email     string    `json:"email" example:"owner@example.test"`
	Role      string    `json:"role" example:"owner"`
	ExpiresAt time.Time `json:"expires_at"`
}

// ChangePasswordRequest is an admin replacing their own password. The current
// password is required even though the session already proves identity: a stolen
// cookie must not be enough to lock the real operator out (ADR 0024 §1).
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password" binding:"required"`
	NewPassword     string `json:"new_password" binding:"required" example:"kata-sandi-baru-yang-panjang"`
}

// ResetPasswordRequest is an owner setting a colleague's password.
type ResetPasswordRequest struct {
	Password string `json:"password" binding:"required" example:"kata-sandi-baru-yang-panjang"`
}

// SetActiveRequest is an owner enabling or disabling an admin. Active is a
// pointer because `false` is a value this body must be able to carry, and
// `binding:"required"` rejects a zero-valued bool (ADR 0024 §5).
type SetActiveRequest struct {
	Active *bool `json:"active" binding:"required" example:"false"`
}

// SetActiveResponse is the answer to an owner enabling or disabling an admin: the
// id it changed and the state it now has, so the panel does not need a second read.
type SetActiveResponse struct {
	ID     uint `json:"id" example:"2"`
	Active bool `json:"active" example:"false"`
}
