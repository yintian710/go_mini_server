package service

import "net/http"

type AppError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"`
}

func (err *AppError) Error() string {
	return err.Message
}

func NewBadRequest(code, message string) *AppError {
	return &AppError{Code: code, Message: message, Status: http.StatusBadRequest}
}

func NewUnauthorized(code, message string) *AppError {
	return &AppError{Code: code, Message: message, Status: http.StatusUnauthorized}
}

func NewForbidden(code, message string) *AppError {
	return &AppError{Code: code, Message: message, Status: http.StatusForbidden}
}

func NewNotFound(code, message string) *AppError {
	return &AppError{Code: code, Message: message, Status: http.StatusNotFound}
}

func NewConflict(code, message string) *AppError {
	return &AppError{Code: code, Message: message, Status: http.StatusConflict}
}
