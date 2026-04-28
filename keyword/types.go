package keyword

// extractResponse는 AI가 반환하는 플랫폼별 키워드 목록입니다.
type extractResponse struct {
	KOCW    []string `json:"kocw"`
	KMOOC   []string `json:"kmooc"`
	YouTube []string `json:"youtube"`
}

// keywordInput은 AI에 전달하는 경량 프로젝트 요약입니다.
type keywordInput struct {
	Project      keywordProject  `json:"project"`
	Overview     string          `json:"overview"`
	Architecture string          `json:"architecture"`
	Modules      []keywordModule `json:"modules"`
}

type keywordProject struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Goal        string `json:"goal"`
}

type keywordModule struct {
	Name         string `json:"name"`
	Language     string `json:"language"`
	ExternalDeps string `json:"externalDeps"`
}

// rawProjectContext는 projectContext.json에서 필요한 필드만 파싱하기 위한 구조체입니다.
type rawProjectContext struct {
	Project struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Goal        string `json:"goal"`
	} `json:"project"`
	Analysis struct {
		Overview     string `json:"overview"`
		Architecture struct {
			Summary string `json:"summary"`
		} `json:"architecture"`
	} `json:"analysis"`
	ModuleDetails []struct {
		Module       string `json:"module"`
		Language     string `json:"language"`
		ExternalDeps string `json:"externalDeps"`
	} `json:"moduleDetails"`
}

