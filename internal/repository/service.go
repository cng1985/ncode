package repository

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cng1985/ncode/internal/config"
	"github.com/cng1985/ncode/internal/identity"
	"github.com/cng1985/ncode/internal/model"
	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
	"gorm.io/gorm"
)

const maxTextFileSize = 1 << 20

var (
	repositorySlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	refPattern            = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,119}$`)
)

var Module = fx.Module("repositories", fx.Provide(New))

type Service struct {
	db   *gorm.DB
	root string
}

func New(db *gorm.DB, cfg config.Config) *Service {
	return &Service{db: db, root: cfg.GitRoot}
}

func (s *Service) RegisterRoutes(r *gin.RouterGroup) {
	r.POST("/repository/create", s.create)
	r.POST("/repository/query/page", s.page)
	r.POST("/repository/query/tree", s.tree)
	r.POST("/repository/query/file", s.file)
}

type createRequest struct {
	ProjectID  int64  `json:"projectId"`
	Name       string `json:"name"`
	Slug       string `json:"slug"`
	Visibility string `json:"visibility"`
}

type repositoryView struct {
	ID            int64  `json:"id"`
	ProjectID     int64  `json:"projectId"`
	ProjectName   string `json:"projectName"`
	Name          string `json:"name"`
	Slug          string `json:"slug"`
	Visibility    string `json:"visibility"`
	DefaultBranch string `json:"defaultBranch"`
	State         string `json:"state"`
	CreatedAt     string `json:"createdAt"`
}

type pageRequest struct {
	Page int `json:"page"`
	Size int `json:"size"`
}

type treeRequest struct {
	RepositoryID int64  `json:"repositoryId"`
	Ref          string `json:"ref"`
	Path         string `json:"path"`
}

type treeEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
	OID  string `json:"oid"`
	Mode string `json:"mode"`
}

func (s *Service) create(c *gin.Context) {
	var req createRequest
	if !bind(c, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Slug = strings.ToLower(strings.TrimSpace(req.Slug))
	if req.Visibility == "" {
		req.Visibility = "private"
	}
	if !model.SafeID(req.ProjectID) || req.Name == "" || len([]rune(req.Name)) > 120 || !repositorySlugPattern.MatchString(req.Slug) || req.Visibility != "private" {
		fail(c, http.StatusBadRequest, 41001, "valid project, name, slug, and visibility are required")
		return
	}
	var project model.Project
	if err := s.db.Where("tenant_id = ?", identity.WorkspaceID(c)).First(&project, req.ProjectID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			fail(c, http.StatusNotFound, 42002, "project not found")
		} else {
			fail(c, http.StatusInternalServerError, 50001, "request could not be completed")
		}
		return
	}
	placeholder := fmt.Sprintf("pending/%d-%d", identity.UserID(c), time.Now().UnixNano())
	repo := model.Repository{TenantID: identity.WorkspaceID(c), ProjectID: project.ID, OwnerUserID: identity.UserID(c), Name: req.Name, Slug: req.Slug, Visibility: req.Visibility, DefaultBranch: "main", DiskPath: placeholder, State: "initializing"}
	if err := s.db.Create(&repo).Error; err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			fail(c, http.StatusConflict, 42002, "repository slug is already in use")
		} else {
			fail(c, http.StatusInternalServerError, 50001, "request could not be completed")
		}
		return
	}
	repo.DiskPath = filepath.Join(s.root, strconv.FormatInt(repo.ID, 10)+".git")
	if err := s.db.Model(&repo).Update("disk_path", repo.DiskPath).Error; err != nil {
		fail(c, http.StatusInternalServerError, 50001, "request could not be completed")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	if err := initialize(ctx, s.root, repo); err != nil {
		_ = s.db.Model(&repo).Update("state", "failed").Error
		fail(c, http.StatusInternalServerError, 50001, "repository initialization failed; retry by creating it again with another slug")
		return
	}
	if err := s.db.Model(&repo).Update("state", "active").Error; err != nil {
		fail(c, http.StatusInternalServerError, 50001, "repository was initialized but could not be activated")
		return
	}
	repo.State = "active"
	success(c, view(repo, project.Name))
}

func (s *Service) page(c *gin.Context) {
	var req pageRequest
	if !bind(c, &req) {
		return
	}
	page, size := req.Page, req.Size
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = 20
	}
	if page < 1 || size < 1 || size > 100 {
		fail(c, http.StatusBadRequest, 41001, "page must be positive and size must be between 1 and 100")
		return
	}
	query := s.db.Model(&model.Repository{}).Where("repositories.tenant_id = ?", identity.WorkspaceID(c))
	var total int64
	if err := query.Count(&total).Error; err != nil {
		fail(c, http.StatusInternalServerError, 50001, "request could not be completed")
		return
	}
	var repositories []model.Repository
	if err := query.Order("repositories.created_at DESC").Offset((page - 1) * size).Limit(size).Find(&repositories).Error; err != nil {
		fail(c, http.StatusInternalServerError, 50001, "request could not be completed")
		return
	}
	data := make([]repositoryView, 0, len(repositories))
	for _, repo := range repositories {
		var project model.Project
		if err := s.db.Select("id", "name").First(&project, repo.ProjectID).Error; err != nil {
			fail(c, http.StatusInternalServerError, 50001, "request could not be completed")
			return
		}
		data = append(data, view(repo, project.Name))
	}
	totalPages := int64(0)
	if total > 0 {
		totalPages = (total + int64(size) - 1) / int64(size)
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "success", "data": data, "page": page, "size": size, "total": total, "totalPage": totalPages, "responseType": "page"})
}

func (s *Service) tree(c *gin.Context) {
	var req treeRequest
	if !bind(c, &req) {
		return
	}
	repo, ok := s.loadRepo(c, req.RepositoryID)
	if !ok {
		return
	}
	dir, valid := cleanRepoPath(req.Path)
	if !valid || len(req.Ref) > 120 {
		fail(c, http.StatusBadRequest, 41001, "repository ref or path is invalid")
		return
	}
	ref := strings.TrimSpace(req.Ref)
	if ref == "" {
		ref = repo.DefaultBranch
	}
	commit, err := resolveCommit(c.Request.Context(), repo, ref)
	if err != nil {
		fail(c, http.StatusNotFound, 42002, "repository ref not found")
		return
	}
	args := []string{"ls-tree", "-z", commit}
	if dir != "" {
		args = []string{"ls-tree", "-z", commit + ":" + dir}
	}
	output, err := gitOutput(c.Request.Context(), repo.DiskPath, args...)
	if err != nil {
		fail(c, http.StatusNotFound, 42002, "repository path not found")
		return
	}
	entries, err := parseTree(dir, output)
	if err != nil {
		fail(c, http.StatusInternalServerError, 50001, "repository tree could not be read")
		return
	}
	success(c, gin.H{"repositoryId": repo.ID, "ref": ref, "commit": commit, "path": dir, "entries": entries})
}

func (s *Service) file(c *gin.Context) {
	var req treeRequest
	if !bind(c, &req) {
		return
	}
	repo, ok := s.loadRepo(c, req.RepositoryID)
	if !ok {
		return
	}
	filePath, valid := cleanRepoPath(req.Path)
	if !valid || filePath == "" || len(req.Ref) > 120 {
		fail(c, http.StatusBadRequest, 41001, "repository ref or file path is invalid")
		return
	}
	ref := strings.TrimSpace(req.Ref)
	if ref == "" {
		ref = repo.DefaultBranch
	}
	commit, err := resolveCommit(c.Request.Context(), repo, ref)
	if err != nil {
		fail(c, http.StatusNotFound, 42002, "repository ref not found")
		return
	}
	objectID, err := gitOutput(c.Request.Context(), repo.DiskPath, "rev-parse", "--verify", "--end-of-options", commit+":"+filePath)
	if err != nil {
		fail(c, http.StatusNotFound, 42002, "repository file not found")
		return
	}
	objectIDText := strings.TrimSpace(string(objectID))
	objectType, err := gitOutput(c.Request.Context(), repo.DiskPath, "cat-file", "-t", objectIDText)
	if err != nil || strings.TrimSpace(string(objectType)) != "blob" {
		fail(c, http.StatusBadRequest, 42001, "repository path is not a regular file")
		return
	}
	sizeOutput, err := gitOutput(c.Request.Context(), repo.DiskPath, "cat-file", "-s", objectIDText)
	if err != nil {
		fail(c, http.StatusInternalServerError, 50001, "repository file could not be read")
		return
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(sizeOutput)), 10, 64)
	if err != nil || size < 0 {
		fail(c, http.StatusInternalServerError, 50001, "repository file could not be read")
		return
	}
	if size > maxTextFileSize {
		fail(c, http.StatusRequestEntityTooLarge, 42001, "file preview is limited to 1 MiB")
		return
	}
	content, err := gitOutput(c.Request.Context(), repo.DiskPath, "cat-file", "blob", objectIDText)
	if err != nil || !utf8.Valid(content) {
		fail(c, http.StatusUnsupportedMediaType, 42001, "binary files cannot be previewed")
		return
	}
	success(c, gin.H{"repositoryId": repo.ID, "ref": ref, "commit": commit, "path": filePath, "content": string(content), "size": size})
}

func (s *Service) loadRepo(c *gin.Context, id int64) (model.Repository, bool) {
	if !model.SafeID(id) {
		fail(c, http.StatusBadRequest, 41001, "repository id is invalid")
		return model.Repository{}, false
	}
	var repo model.Repository
	if err := s.db.Where("tenant_id = ? AND state = ?", identity.WorkspaceID(c), "active").First(&repo, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			fail(c, http.StatusNotFound, 42002, "repository not found")
		} else {
			fail(c, http.StatusInternalServerError, 50001, "request could not be completed")
		}
		return model.Repository{}, false
	}
	if _, err := s.resolvePath(repo.DiskPath); err != nil {
		fail(c, http.StatusInternalServerError, 50001, "repository storage is unavailable")
		return model.Repository{}, false
	}
	return repo, true
}

func (s *Service) resolvePath(candidate string) (string, error) {
	root, err := filepath.Abs(s.root)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("repository path is outside configured storage")
	}
	return resolved, nil
}

func initialize(ctx context.Context, root string, repo model.Repository) error {
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	barePath, err := filepath.Abs(repo.DiskPath)
	if err != nil {
		return err
	}
	rootPath, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(rootPath, barePath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("repository path is outside configured storage")
	}
	if _, err := os.Stat(barePath); err == nil {
		if err := os.RemoveAll(barePath); err != nil {
			return err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if _, err := gitOutput(ctx, "", "init", "--bare", "--initial-branch=main", barePath); err != nil {
		return err
	}
	seedDir, err := os.MkdirTemp(rootPath, ".seed-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(seedDir)
	if err := os.Chmod(seedDir, 0700); err != nil {
		return err
	}
	if _, err := gitOutput(ctx, seedDir, "init", "--initial-branch=main"); err != nil {
		return err
	}
	readme := fmt.Sprintf("# %s\n\nRepository for project delivery.\n", repo.Name)
	if err := os.WriteFile(filepath.Join(seedDir, "README.md"), []byte(readme), 0600); err != nil {
		return err
	}
	if _, err := gitOutput(ctx, seedDir, "add", "--", "README.md"); err != nil {
		return err
	}
	if _, err := gitOutput(ctx, seedDir, "-c", "user.name=NCode", "-c", "user.email=noreply@ncode.invalid", "-c", "commit.gpgsign=false", "commit", "-m", "Initial commit"); err != nil {
		return err
	}
	if _, err := gitOutput(ctx, seedDir, "remote", "add", "origin", barePath); err != nil {
		return err
	}
	if _, err := gitOutput(ctx, seedDir, "push", "origin", "main"); err != nil {
		return err
	}
	if _, err := gitOutput(ctx, "", "--git-dir", barePath, "config", "receive.denyNonFastForwards", "true"); err != nil {
		return err
	}
	if _, err := gitOutput(ctx, "", "--git-dir", barePath, "config", "receive.denyDeletes", "true"); err != nil {
		return err
	}
	return nil
}

func gitOutput(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git operation failed: %s", strings.TrimSpace(string(output)))
	}
	return output, nil
}

func resolveCommit(ctx context.Context, repo model.Repository, ref string) (string, error) {
	if !validRef(ref) {
		return "", fmt.Errorf("invalid ref")
	}
	output, err := gitOutput(ctx, repo.DiskPath, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	commit := strings.TrimSpace(string(output))
	if len(commit) < 40 || len(commit) > 64 {
		return "", fmt.Errorf("invalid commit id")
	}
	return commit, nil
}

func validRef(ref string) bool {
	return refPattern.MatchString(ref) && !strings.Contains(ref, "..") && !strings.Contains(ref, "//") && !strings.HasSuffix(ref, ".") && !strings.HasSuffix(ref, "/") && !strings.Contains(ref, "@{")
}

func cleanRepoPath(value string) (string, bool) {
	if len(value) > 1024 || strings.Contains(value, "\\") || strings.Contains(value, ":") || strings.ContainsRune(value, 0) {
		return "", false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." {
			return "", false
		}
	}
	clean := path.Clean(value)
	if clean == "." {
		clean = ""
	}
	if strings.HasPrefix(clean, "../") || clean == ".." || strings.HasPrefix(clean, "/") {
		return "", false
	}
	return clean, true
}

func parseTree(base string, output []byte) ([]treeEntry, error) {
	entries := make([]treeEntry, 0)
	for _, record := range strings.Split(string(output), "\x00") {
		if record == "" {
			continue
		}
		parts := strings.SplitN(record, "\t", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("malformed git tree entry")
		}
		metadata := strings.Fields(parts[0])
		if len(metadata) != 3 {
			return nil, fmt.Errorf("malformed git tree metadata")
		}
		entryPath := parts[1]
		if base != "" {
			entryPath = path.Join(base, entryPath)
		}
		entries = append(entries, treeEntry{Name: path.Base(parts[1]), Path: entryPath, Type: metadata[1], OID: metadata[2], Mode: metadata[0]})
	}
	return entries, nil
}

func view(repo model.Repository, projectName string) repositoryView {
	return repositoryView{ID: repo.ID, ProjectID: repo.ProjectID, ProjectName: projectName, Name: repo.Name, Slug: repo.Slug, Visibility: repo.Visibility, DefaultBranch: repo.DefaultBranch, State: repo.State, CreatedAt: repo.CreatedAt.UTC().Format(time.RFC3339)}
}

func bind(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	if err := c.ShouldBindJSON(target); err != nil {
		fail(c, http.StatusBadRequest, 41001, "invalid request")
		return false
	}
	return true
}

func oneOf(value string, values ...string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func success(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "success", "data": data})
}

func fail(c *gin.Context, status, code int, message string) {
	c.JSON(status, gin.H{"code": code, "msg": message, "data": nil})
}
