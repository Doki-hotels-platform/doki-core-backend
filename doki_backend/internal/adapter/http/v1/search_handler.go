package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"doki-backend/internal/adapter/http/v1/dto"
)

type SearchHandler struct {
	pool *pgxpool.Pool
}

func NewSearchHandler(pool *pgxpool.Pool) *SearchHandler {
	return &SearchHandler{pool: pool}
}

// SearchProperties handles GET /v1/properties/search.
func (h *SearchHandler) SearchProperties(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	q := r.URL.Query()

	// 1. Parse and Validate Date Range
	checkInStr := q.Get("check_in")
	checkOutStr := q.Get("check_out")

	if checkInStr == "" || checkOutStr == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "INVALID_PARAMETERS",
			Message: "Both check_in and check_out parameters (YYYY-MM-DD) are required",
		})
		return
	}

	checkIn, err := time.Parse("2006-01-02", checkInStr)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "INVALID_PARAMETERS",
			Message: "check_in must be formatted as YYYY-MM-DD",
		})
		return
	}

	checkOut, err := time.Parse("2006-01-02", checkOutStr)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "INVALID_PARAMETERS",
			Message: "check_out must be formatted as YYYY-MM-DD",
		})
		return
	}

	today := time.Now().UTC().Truncate(24 * time.Hour)
	if !checkOut.After(checkIn) || checkIn.Before(today) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "INVALID_DATE_RANGE",
			Message: "check_out must be after check_in and stay dates cannot be in the past",
		})
		return
	}

	// 2. Parse Pagination and Filters
	guests := 2
	if gStr := q.Get("guests"); gStr != "" {
		if g, err := strconv.Atoi(gStr); err == nil && g > 0 {
			guests = g
		}
	}

	page := 1
	if pStr := q.Get("page"); pStr != "" {
		if p, err := strconv.Atoi(pStr); err == nil && p > 0 {
			page = p
		}
	}

	pageSize := 20
	if psStr := q.Get("page_size"); psStr != "" {
		if ps, err := strconv.Atoi(psStr); err == nil && ps > 0 && ps <= 50 {
			pageSize = ps
		}
	}

	region := q.Get("region")
	city := q.Get("city")

	// 3. Query PostgreSQL for Available Inventory
	res, err := h.queryAvailableProperties(r.Context(), checkIn, checkOut, region, city, guests, page, pageSize)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "INTERNAL_SERVER_ERROR",
			Message: "Failed to execute availability search",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(res)
}

func (h *SearchHandler) queryAvailableProperties(
	ctx context.Context,
	checkIn, checkOut time.Time,
	region, city string,
	guests, page, pageSize int,
) (*dto.SearchPropertiesResponse, error) {
	stayNights := int(checkOut.Sub(checkIn).Hours() / 24)

	// SQL availability search query aggregating daily allocation bounds across stay window
	query := `
		WITH room_availability AS (
			SELECT 
				p.id AS property_id,
				p.code AS property_code,
				p.name AS property_name,
				p.category AS property_category,
				p.city,
				p.region,
				p.latitude,
				p.longitude,
				p.base_currency,
				rt.id AS room_type_id,
				rt.name AS room_type_name,
				rt.capacity,
				MIN(da.total_units - da.allocated_count - da.blocked_count) AS min_available,
				AVG(da.rate_minor)::BIGINT AS avg_rate_minor,
				COUNT(da.stay_date) AS matching_nights
			FROM property.property p
			JOIN property.room_type rt ON rt.property_id = p.id
			JOIN inventory.daily_allocations da ON da.property_id = p.id AND da.room_type_id = rt.id
			WHERE p.status = 'ACTIVE'
				AND rt.capacity >= $1
				AND da.stay_date >= $2 AND da.stay_date < $3
				AND ($4 = '' OR p.region ILIKE '%' || $4 || '%')
				AND ($5 = '' OR p.city ILIKE '%' || $5 || '%')
			GROUP BY p.id, p.code, p.name, p.category, p.city, p.region, p.latitude, p.longitude, p.base_currency, rt.id, rt.name, rt.capacity
			HAVING COUNT(da.stay_date) = $6 AND MIN(da.total_units - da.allocated_count - da.blocked_count) > 0
		)
		SELECT 
			property_id, property_code, property_name, property_category,
			city, region, latitude, longitude, base_currency,
			room_type_id, room_type_name, capacity, min_available, avg_rate_minor
		FROM room_availability
		ORDER BY property_name ASC, avg_rate_minor ASC;
	`

	rows, err := h.pool.Query(ctx, query, guests, checkIn.Format("2006-01-02"), checkOut.Format("2006-01-02"), region, city, stayNights)
	if err != nil {
		return nil, fmt.Errorf("execute availability search: %w", err)
	}
	defer rows.Close()

	propertyMap := make(map[string]*dto.PropertySearchResult)
	var orderedPropertyIDs []string

	for rows.Next() {
		var (
			propID, propCode, propName, propCategory, propCity, propRegion, baseCurrency string
			lat, lon                                                                     *float64
			rtID, rtName                                                                 string
			capacity, minAvailable                                                       int
			avgRateMinor                                                                 int64
		)

		err := rows.Scan(
			&propID, &propCode, &propName, &propCategory,
			&propCity, &propRegion, &lat, &lon, &baseCurrency,
			&rtID, &rtName, &capacity, &minAvailable, &avgRateMinor,
		)
		if err != nil {
			return nil, fmt.Errorf("scan search result row: %w", err)
		}

		roomTypeDTO := dto.AvailableRoomType{
			RoomTypeID:       rtID,
			Name:             rtName,
			Capacity:         capacity,
			NightlyRateMinor: avgRateMinor,
			Currency:         baseCurrency,
			UnitsAvailable:   minAvailable,
		}

		if prop, exists := propertyMap[propID]; exists {
			prop.AvailableRoomTypes = append(prop.AvailableRoomTypes, roomTypeDTO)
		} else {
			p := &dto.PropertySearchResult{
				PropertyID:         propID,
				Code:               propCode,
				Name:               propName,
				Category:           propCategory,
				City:               propCity,
				Region:             propRegion,
				Latitude:           lat,
				Longitude:          lon,
				AvailableRoomTypes: []dto.AvailableRoomType{roomTypeDTO},
			}
			propertyMap[propID] = p
			orderedPropertyIDs = append(orderedPropertyIDs, propID)
		}
	}

	totalResults := len(orderedPropertyIDs)

	// Apply pagination slicing
	start := (page - 1) * pageSize
	if start > totalResults {
		start = totalResults
	}
	end := start + pageSize
	if end > totalResults {
		end = totalResults
	}

	var results []dto.PropertySearchResult
	for i := start; i < end; i++ {
		results = append(results, *propertyMap[orderedPropertyIDs[i]])
	}

	return &dto.SearchPropertiesResponse{
		Results:      results,
		Page:         page,
		PageSize:     pageSize,
		TotalResults: totalResults,
	}, nil
}
