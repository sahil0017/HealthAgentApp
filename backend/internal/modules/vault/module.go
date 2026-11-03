// Package vault owns documents: upload, processing results, versions, search and history (module 02).
//
// It may import internal/platform, internal/domain and internal/repo only, never another module.
package vault

import (
	"cortex/backend/internal/platform/core"
	"cortex/backend/internal/platform/module"

	"github.com/go-chi/chi/v5"
)

// Module implements module.Module.
type Module struct {
	module.Base
	*core.Core
}

func New(c *core.Core) *Module { return &Module{Core: c} }

func (m *Module) Name() string { return "vault" }

func (m *Module) Authed(r chi.Router) {
	r.Post("/profiles/{profileID}/documents", m.uploadDocument)
	r.Post("/profiles/{profileID}/documents/upload-intents", m.createUploadIntent)
	r.Get("/profiles/{profileID}/documents", m.listDocuments)
	r.Get("/profiles/{profileID}/documents/search", m.searchDocuments)
	r.Get("/profiles/{profileID}/documents/{documentID}", m.getDocument)
	r.Get("/profiles/{profileID}/documents/{documentID}/file", m.downloadDocument)
	r.Post("/profiles/{profileID}/documents/{documentID}/approve", m.approveDocument)
	r.Post("/profiles/{profileID}/documents/{documentID}/retry", m.retryDocument)
	r.Post("/documents/{documentID}/complete", m.completeUpload)
	r.Get("/documents/{documentID}", m.getDocumentByID)
	r.Get("/documents/{documentID}/versions", m.documentVersions)
	r.Get("/documents/{documentID}/history", m.documentHistory)
	r.Get("/documents/{documentID}/extraction/pages", m.documentExtractionPages)
	r.Get("/documents/{documentID}/download", m.documentDownloadURL)
	r.Patch("/documents/{documentID}", m.updateDocument)
	r.Delete("/documents/{documentID}", m.deleteDocument)
}
