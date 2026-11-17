package handlers_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
)

var _ = Describe("GroupHandler", func() {
	var (
		e       *echo.Echo
		gs      *fakeGroupStore
		handler *handlers.GroupHandler
	)

	BeforeEach(func() {
		e = echo.New()
		gs = &fakeGroupStore{}
		handler = handlers.NewGroupHandler(gs)
	})

	Describe("Create", func() {
		It("should create a group", func() {
			body := `{"name":"production","description":"Production nodes"}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/groups", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := handler.Create(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusCreated))

			var group store.NodeGroup
			Expect(json.Unmarshal(rec.Body.Bytes(), &group)).To(Succeed())
			Expect(group.Name).To(Equal("production"))
			Expect(group.ID).NotTo(BeEmpty())
		})

		It("should reject group without name", func() {
			body := `{"description":"No name"}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/groups", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := handler.Create(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusBadRequest))
		})
	})

	Describe("List", func() {
		It("should list all groups", func() {
			gs.groups = []*store.NodeGroup{
				{ID: "grp-1", Name: "prod"},
				{ID: "grp-2", Name: "staging"},
			}
			req := httptest.NewRequest(http.MethodGet, "/api/v1/groups", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := handler.List(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusOK))

			var groups []*store.NodeGroup
			Expect(json.Unmarshal(rec.Body.Bytes(), &groups)).To(Succeed())
			Expect(groups).To(HaveLen(2))
		})
	})

	Describe("node counts", func() {
		BeforeEach(func() {
			gs.groups = []*store.NodeGroup{
				{ID: "grp-1", Name: "prod"},
				{ID: "grp-2", Name: "staging"},
			}
			gs.counts = map[string]int{"grp-1": 2}
		})

		It("List returns node_count for every group, 0 included", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/groups", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			Expect(handler.List(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusOK))

			var groups []map[string]any
			Expect(json.Unmarshal(rec.Body.Bytes(), &groups)).To(Succeed())
			Expect(groups).To(HaveLen(2))
			Expect(groups[0]).To(HaveKeyWithValue("node_count", BeNumerically("==", 2)))
			Expect(groups[1]).To(HaveKeyWithValue("node_count", BeNumerically("==", 0)))
		})

		It("List returns 500 when counting fails", func() {
			gs.countErr = errors.New("boom")
			req := httptest.NewRequest(http.MethodGet, "/api/v1/groups", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			Expect(handler.List(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusInternalServerError))
			Expect(rec.Body.String()).To(ContainSubstring("failed to count group nodes"))
		})

		It("Get returns node_count for the group", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/groups/grp-1", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues("grp-1")

			Expect(handler.Get(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusOK))

			var group map[string]any
			Expect(json.Unmarshal(rec.Body.Bytes(), &group)).To(Succeed())
			Expect(group).To(HaveKeyWithValue("node_count", BeNumerically("==", 2)))
		})
	})

	Describe("Get", func() {
		It("should return a group by ID", func() {
			gs.groups = []*store.NodeGroup{
				{ID: "grp-1", Name: "prod"},
			}
			req := httptest.NewRequest(http.MethodGet, "/api/v1/groups/grp-1", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues("grp-1")

			err := handler.Get(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusOK))
		})

		It("should return 404 for missing group", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/groups/missing", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues("missing")

			err := handler.Get(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusNotFound))
		})
	})

	Describe("Update", func() {
		It("should update a group", func() {
			gs.groups = []*store.NodeGroup{
				{ID: "grp-1", Name: "prod", Description: "old desc"},
			}
			body := `{"name":"production","description":"Production environment"}`
			req := httptest.NewRequest(http.MethodPut, "/api/v1/groups/grp-1", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues("grp-1")

			err := handler.Update(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusOK))

			var group store.NodeGroup
			Expect(json.Unmarshal(rec.Body.Bytes(), &group)).To(Succeed())
			Expect(group.Name).To(Equal("production"))
			Expect(group.Description).To(Equal("Production environment"))
		})

		update := func(body string) store.NodeGroup {
			req := httptest.NewRequest(http.MethodPut, "/api/v1/groups/grp-1", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues("grp-1")

			Expect(handler.Update(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusOK))

			var group store.NodeGroup
			Expect(json.Unmarshal(rec.Body.Bytes(), &group)).To(Succeed())
			return group
		}

		It("clears the description when it is sent as an empty string", func() {
			gs.groups = []*store.NodeGroup{
				{ID: "grp-1", Name: "prod", Description: "old desc"},
			}

			group := update(`{"name":"prod","description":""}`)
			Expect(group.Description).To(BeEmpty())
			Expect(gs.groups[0].Description).To(BeEmpty())
		})

		It("leaves the description alone when the field is omitted", func() {
			gs.groups = []*store.NodeGroup{
				{ID: "grp-1", Name: "prod", Description: "old desc"},
			}

			group := update(`{"name":"production"}`)
			Expect(group.Name).To(Equal("production"))
			Expect(group.Description).To(Equal("old desc"))
			Expect(gs.groups[0].Description).To(Equal("old desc"))
		})

		It("leaves the name alone when it is sent as an empty string", func() {
			gs.groups = []*store.NodeGroup{
				{ID: "grp-1", Name: "prod", Description: "old desc"},
			}

			group := update(`{"name":"","description":"new desc"}`)
			Expect(group.Name).To(Equal("prod"))
			Expect(group.Description).To(Equal("new desc"))
		})
	})

	Describe("Delete", func() {
		It("should delete a group", func() {
			gs.groups = []*store.NodeGroup{
				{ID: "grp-1", Name: "prod"},
			}
			req := httptest.NewRequest(http.MethodDelete, "/api/v1/groups/grp-1", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues("grp-1")

			err := handler.Delete(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusNoContent))
		})
	})
})

var _ = Describe("GroupHandler with the database store", func() {
	var (
		e       *echo.Echo
		handler *handlers.GroupHandler
	)

	BeforeEach(func() {
		e = echo.New()
		s, err := gormstore.New(filepath.Join(GinkgoT().TempDir(), "groups.db"))
		Expect(err).NotTo(HaveOccurred())
		handler = handlers.NewGroupHandler(&gormstore.GroupStoreAdapter{S: s})
	})

	create := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/groups", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		Expect(handler.Create(e.NewContext(req, rec))).To(Succeed())
		return rec
	}

	createGroup := func(name string) store.NodeGroup {
		rec := create(`{"name":"` + name + `"}`)
		Expect(rec.Code).To(Equal(http.StatusCreated))
		var g store.NodeGroup
		Expect(json.Unmarshal(rec.Body.Bytes(), &g)).To(Succeed())
		return g
	}

	errorOf := func(rec *httptest.ResponseRecorder) string {
		var body map[string]string
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
		return body["error"]
	}

	It("answers 409 when creating a group whose name is taken", func() {
		createGroup("prod")

		rec := create(`{"name":"prod"}`)
		Expect(rec.Code).To(Equal(http.StatusConflict))
		Expect(errorOf(rec)).To(Equal(`a group named "prod" already exists`))
	})

	It("answers 409 when renaming a group to a name that is taken", func() {
		createGroup("prod")
		staging := createGroup("staging")

		req := httptest.NewRequest(http.MethodPut, "/api/v1/groups/"+staging.ID, strings.NewReader(`{"name":"prod"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(staging.ID)

		Expect(handler.Update(c)).To(Succeed())
		Expect(rec.Code).To(Equal(http.StatusConflict))
		Expect(errorOf(rec)).To(Equal(`a group named "prod" already exists`))
	})
})
