package query

type Page struct {
	Page     int `form:"page"`
	PageSize int `form:"page_size"`
}

func (p Page) Normalize() (page, pageSize, offset int) {
	page = p.Page
	pageSize = p.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 50 {
		pageSize = 50
	}
	offset = (page - 1) * pageSize
	return page, pageSize, offset
}

type PageResult struct {
	List     interface{} `json:"list"`
	Total    int64       `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
}
