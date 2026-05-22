package library

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// REST surface for the knowledge-base modules (P-library-0a).
//
// Routes are wired in app.go under /api/library/{devices,firmwares,functions}.
// Auth: writes (POST / DELETE / upsert) require AdminMiddleware;
// reads (GET / list / lookup / search / match) only require login.
// Data is GLOBAL — no workspace gating.

// ---------- devices ----------

func (p *Plugin) handleRevDeviceList(c *gin.Context) {
	cpid, _ := strconv.Atoi(c.Query("cpid"))
	bdid := -1
	if v := strings.TrimSpace(c.Query("bdid")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			bdid = n
		}
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	items, err := p.listRevDevices(cpid, bdid, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"devices": items})
}

func (p *Plugin) handleRevDeviceGet(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 id"})
		return
	}
	d, err := p.getRevDevice(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}
	if d == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"device": d})
}

// POST /api/library/devices — upsert by ECID. Admin-only.
func (p *Plugin) handleRevDeviceUpsert(c *gin.Context) {
	var d RevDevice
	if err := c.ShouldBindJSON(&d); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body: " + err.Error()})
		return
	}
	if d.CPID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cpid required"})
		return
	}
	out, err := p.upsertRevDevice(&d, time.Now().UTC())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"device": out})
}

func (p *Plugin) handleRevDeviceDelete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 id"})
		return
	}
	ok, err := p.deleteRevDevice(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (p *Plugin) handleRevDeviceRecent(c *gin.Context) {
	days, _ := strconv.Atoi(c.Query("days"))
	limit, _ := strconv.Atoi(c.Query("limit"))
	items, err := p.recentRevDevices(days, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"devices": items, "days": days})
}

// ---------- firmwares ----------

func (p *Plugin) handleRevFirmwareList(c *gin.Context) {
	kind := c.Query("kind")
	version := c.Query("version")
	cpid, _ := strconv.Atoi(c.Query("cpid"))
	limit, _ := strconv.Atoi(c.Query("limit"))
	items, err := p.listRevFirmwares(kind, version, cpid, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"firmwares": items})
}

func (p *Plugin) handleRevFirmwareGet(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 id"})
		return
	}
	f, err := p.getRevFirmware(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}
	if f == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"firmware": f})
}

// POST /api/library/firmwares — admin: ingest metadata. v1 takes
// blob_uri + blob_sha256 from the caller (operator uploaded the
// blob separately, e.g. via rsync into ~/polar/firmwares/). Next
// PR adds multipart upload + auto sha256.
func (p *Plugin) handleRevFirmwareCreate(c *gin.Context) {
	userID, _ := c.Get("user_id")
	var f RevFirmware
	if err := c.ShouldBindJSON(&f); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body: " + err.Error()})
		return
	}
	if uid, _ := userID.(string); uid != "" && f.AddedBy == "" {
		f.AddedBy = uid
	}
	out, err := p.insertRevFirmware(&f, time.Now().UTC())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"firmware": out})
}

func (p *Plugin) handleRevFirmwareDelete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 id"})
		return
	}
	ok, err := p.deleteRevFirmware(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GET /api/library/firmwares/matching?cpid=X&bdid=Y — "what fits"
func (p *Plugin) handleRevFirmwareMatching(c *gin.Context) {
	cpid, _ := strconv.Atoi(c.Query("cpid"))
	bdid := -1
	if v := strings.TrimSpace(c.Query("bdid")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			bdid = n
		}
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	items, err := p.matchingRevFirmwares(cpid, bdid, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"firmwares": items, "cpid": cpid, "bdid": bdid})
}

// ---------- functions ----------

func (p *Plugin) handleRevFunctionList(c *gin.Context) {
	firmwareID, err := strconv.ParseInt(c.Query("firmware_id"), 10, 64)
	if err != nil || firmwareID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "firmware_id required"})
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	items, err := p.listRevFunctions(firmwareID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"functions": items})
}

func (p *Plugin) handleRevFunctionGet(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 id"})
		return
	}
	fn, err := p.getRevFunction(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}
	if fn == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"function": fn})
}

// GET /api/library/functions/lookup-by-address?firmware_id=X&address=Y
// MOST-used during debug loops. Address may be decimal OR 0x-prefixed hex.
func (p *Plugin) handleRevFunctionLookupByAddress(c *gin.Context) {
	firmwareID, err := strconv.ParseInt(c.Query("firmware_id"), 10, 64)
	if err != nil || firmwareID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "firmware_id required"})
		return
	}
	addr, err := parseFlexibleInt64(c.Query("address"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "address must be int or 0x... hex"})
		return
	}
	fn, err := p.lookupRevFunctionByAddress(firmwareID, addr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}
	if fn == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no function at that address"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"function": fn})
}

func (p *Plugin) handleRevFunctionLookupBySymbol(c *gin.Context) {
	firmwareID, err := strconv.ParseInt(c.Query("firmware_id"), 10, 64)
	if err != nil || firmwareID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "firmware_id required"})
		return
	}
	name := strings.TrimSpace(c.Query("name"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name required"})
		return
	}
	fn, err := p.lookupRevFunctionBySymbol(firmwareID, name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}
	if fn == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no function with that symbol"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"function": fn})
}

func (p *Plugin) handleRevFunctionSearch(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "q required"})
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	items, err := p.searchRevFunctions(q, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"functions": items, "q": q})
}

func (p *Plugin) handleRevFunctionMatchSignature(c *gin.Context) {
	prefix := strings.TrimSpace(c.Query("prefix_hex"))
	if prefix == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "prefix_hex required"})
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	items, err := p.matchRevFunctionSignature(prefix, limit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"functions": items, "prefix_hex": prefix})
}

func (p *Plugin) handleRevFunctionCreate(c *gin.Context) {
	userID, _ := c.Get("user_id")
	var body RevFunction
	// Accept `address` as decimal OR 0x... hex by decoding from
	// raw json instead of binding directly.
	raw, _ := c.GetRawData()
	if err := json.Unmarshal(raw, &body); err != nil {
		// retry with flexible address parsing
		var alt map[string]any
		if err2 := json.Unmarshal(raw, &alt); err2 == nil {
			if v, ok := alt["address"].(string); ok {
				addr, err3 := parseFlexibleInt64(v)
				if err3 == nil {
					delete(alt, "address")
					rebound, _ := json.Marshal(alt)
					_ = json.Unmarshal(rebound, &body)
					body.Address = addr
				} else {
					c.JSON(http.StatusBadRequest, gin.H{"error": "address must be int or 0x... hex"})
					return
				}
			} else {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
				return
			}
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body: " + err.Error()})
			return
		}
	}
	if uid, _ := userID.(string); uid != "" && body.AddedBy == "" {
		body.AddedBy = uid
	}
	out, err := p.insertRevFunction(&body, time.Now().UTC())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"function": out})
}

func (p *Plugin) handleRevFunctionDelete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 id"})
		return
	}
	ok, err := p.deleteRevFunction(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// parseFlexibleInt64 accepts decimal ("12345") or 0x-prefixed hex
// ("0x80001234") forms. Used for `address` since addresses are
// universally written in hex but JSON has no native hex literal.
func parseFlexibleInt64(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		return strconv.ParseInt(s[2:], 16, 64)
	}
	return strconv.ParseInt(s, 10, 64)
}
