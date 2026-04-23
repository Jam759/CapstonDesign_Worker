package ai

import (
	"fmt"
	"strings"
	"unicode"
)

// sanitize는 JSON 문자열에 포함 불가한 제어 문자를 제거합니다.
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

// BatchModuleAnalysisPrompt는 여러 모듈을 한 번에 분석하는 프롬프트를 생성합니다.
// modulesJSON: [{module, nodes, imports, contents}, ...] 형식의 JSON
func BatchModuleAnalysisPrompt(language string, modulesJSON string) Prompt {
	modulesJSON = sanitize(modulesJSON)

	user := fmt.Sprintf(`다음은 여러 모듈의 코드 분석 정보입니다. (언어: %s)

## 모듈 데이터 (배열)
%s

위 각 모듈에 대해 다음을 JSON 배열로 응답해주세요:
[
  {
    "module": "모듈명",
    "summary": "모듈의 역할, 목적, 책임 범위를 상세히 설명",
    "functions": "모든 함수/메서드에 대해 각각의 역할, 파라미터 의미, 반환값, 호출 흐름, 사이드 이펙트를 상세히 설명",
    "types": "모든 클래스/구조체/인터페이스/열거형에 대해 각 필드/속성의 용도, 타입 간 관계, 구현체 목록, 상속/구현 관계, 설계 의도를 상세히 설명",
    "variables": "모든 변수/상수/프로퍼티에 대해 용도, 초기화 시점, 변경 조건, 스코프, 다른 코드와의 관계를 상세히 설명",
    "imports": "각 외부 의존성/라이브러리가 어떤 목적으로 사용되는지, 어떤 함수에서 사용하는지 상세히 설명",
    "concurrency": "비동기/동시성 패턴 사용 여부와 구체적인 사용 방식을 상세히 설명. 없으면 '없음'",
    "patterns": "디자인 패턴, 에러/예외 처리 패턴, 리소스 관리 패턴 등 구현 패턴을 상세히 설명. 없으면 '없음'",
    "responsibilities": "이 모듈이 담당하는 구체적 책임을 목록으로 상세히 설명",
    "externalDeps": "이 모듈이 의존하는 외부 시스템/라이브러리와 연동 방식을 상세히 설명. 없으면 '없음'",
    "risks": "이 모듈 고유의 리스크, 취약점, 개선 필요 사항을 상세히 설명. 없으면 '없음'"
  }
]
반드시 위 JSON 배열 형식만 응답하세요. 입력된 모듈 수만큼 배열 원소를 반환하세요.`, language, modulesJSON)

	system := "당신은 시니어 소프트웨어 엔지니어이자 코드 분석 전문가입니다. 주어진 코드를 빠짐없이 분석하여 JSON 배열로 응답합니다. 반드시 요청된 JSON 배열 형식으로만 응답하세요."

	return Prompt{User: user, System: system}
}

// ModuleAnalysisPrompt는 모듈별 코드 분석 프롬프트를 생성합니다.
func ModuleAnalysisPrompt(modName string, language string, nodesJSON string, importsJSON string, contentsJSON string) Prompt {
	nodesJSON = sanitize(nodesJSON)
	importsJSON = sanitize(importsJSON)
	contentsJSON = sanitize(contentsJSON)

	user := fmt.Sprintf(`다음은 "%s" 모듈의 코드 분석 정보입니다. (언어: %s)

## 코드 구조 (노드)
%s

## Import/의존성 목록
%s

## 소스코드
%s

위 정보를 기반으로 이 모듈에 대해 다음을 JSON으로 응답해주세요:
{
  "summary": "모듈의 역할, 목적, 책임 범위를 상세히 설명",
  "functions": "모든 함수/메서드에 대해 각각의 역할, 파라미터 의미, 반환값, 호출 흐름, 사이드 이펙트를 상세히 설명",
  "types": "모든 클래스/구조체/인터페이스/열거형에 대해 각 필드/속성의 용도, 타입 간 관계, 구현체 목록, 상속/구현 관계, 설계 의도를 상세히 설명",
  "variables": "모든 변수/상수/프로퍼티에 대해 용도, 초기화 시점, 변경 조건, 스코프, 다른 코드와의 관계를 상세히 설명",
  "imports": "각 외부 의존성/라이브러리가 어떤 목적으로 사용되는지, 어떤 함수에서 사용하는지 상세히 설명",
  "concurrency": "비동기/동시성 패턴 사용 여부와 구체적인 사용 방식을 상세히 설명. 없으면 '없음'",
  "patterns": "디자인 패턴, 에러/예외 처리 패턴, 리소스 관리 패턴, 빌드/컴파일 분기 등 구현 패턴을 상세히 설명. 없으면 '없음'",
  "responsibilities": "이 모듈이 담당하는 구체적 책임을 목록으로 상세히 설명",
  "externalDeps": "이 모듈이 의존하는 외부 시스템/라이브러리와 연동 방식을 상세히 설명. 없으면 '없음'",
  "risks": "이 모듈 고유의 리스크, 취약점, 개선 필요 사항을 상세히 설명. 없으면 '없음'"
}
반드시 위 JSON 형식만 응답하세요. 각 항목은 최대한 상세하게 작성하세요.`, modName, language, nodesJSON, importsJSON, contentsJSON)

	system := "당신은 시니어 소프트웨어 엔지니어이자 코드 분석 전문가입니다. 주어진 코드를 빠짐없이 분석하여, 코드를 직접 보지 않은 개발자도 이 모듈을 완전히 이해할 수 있을 정도로 상세한 설명을 제공합니다. 반드시 요청된 JSON 형식으로만 응답하세요."

	return Prompt{User: user, System: system}
}

// ModuleChunkPrompt는 청크 파일 기반 모듈 일괄 분석 프롬프트를 생성합니다.
// chunk.json 형식: {"language": "...", "modules": [{"module": "...", "nodes": [...], "imports": [...], "contents": [...]}]}
func ModuleChunkPrompt() Prompt {
	user := `첨부된 chunk.json 파일에는 여러 모듈의 코드 데이터가 포함되어 있습니다.
파일 형식: {"language": "...", "modules": [{"module": "모듈명", "nodes": [...], "imports": [...], "contents": [...]}]}

각 모듈을 빠짐없이 분석하여 아래 JSON 배열로 응답하세요.
- 코드에 존재하는 함수/타입/변수만 포함하고, 없으면 빈 배열([])로 응답하세요.
- 각 항목은 코드를 직접 보지 않은 시니어 개발자가 바로 이해하고 기여할 수 있을 수준으로 작성하세요.

[
  {
    "module": "모듈명",
    "summary": "이 모듈의 역할, 목적, 담당 책임 범위를 3~5문장으로 설명. 다른 모듈과의 관계, 이 모듈이 없으면 어떤 기능이 동작하지 않는지 포함",
    "functions": [
      {
        "name": "함수명",
        "visibility": "exported 또는 unexported",
        "description": "이 함수가 하는 일, 언제/왜 호출되는지, 내부 처리 흐름 요약을 3~5문장으로 상세히",
        "parameters": [
          {"name": "파라미터명", "type": "정확한 타입", "description": "이 값이 의미하는 것, 유효 범위, 주의사항"}
        ],
        "returns": "반환값의 의미, 각 반환 케이스(정상/오류) 설명",
        "isAsync": false,
        "complexity": "low|medium|high — 한 줄 이유 (예: high — 분기가 7개이며 외부 호출 3개 포함)",
        "sideEffects": "DB 저장, 파일 쓰기, 외부 API 호출, 전역 상태 변경 등. 없으면 '없음'",
        "calls": ["직접 호출하는 주요 함수명 (패키지.함수 형식)"],
        "calledBy": ["이 함수를 호출하는 같은 모듈 내 함수명. 알 수 없으면 빈 배열"]
      }
    ],
    "types": [
      {
        "name": "타입명",
        "kind": "struct|interface|enum|type",
        "description": "이 타입의 설계 의도, 어떤 역할을 모델링하는지, 생명주기 설명",
        "fields": [
          {"name": "필드명", "type": "정확한 타입", "description": "이 필드의 용도, 언제 설정되는지, null/zero 허용 여부"}
        ],
        "methods": ["이 타입에 정의된 메서드명 목록 (있는 경우)"],
        "implements": ["이 타입이 구현하는 인터페이스명 목록 (있는 경우)"],
        "embedded": ["임베드된 타입명 목록 (있는 경우)"]
      }
    ],
    "variables": [
      {
        "name": "변수명/상수명",
        "kind": "variable|constant",
        "description": "용도, 초기값 의미, 어떤 함수에서 읽고 쓰는지, 변경 조건",
        "scope": "package|function|global"
      }
    ],
    "imports": [
      {
        "path": "import 경로",
        "alias": "별칭 (없으면 빈 문자열)",
        "purpose": "이 패키지가 이 모듈에서 구체적으로 어떤 기능에 사용되는지 (예: 'S3 파일 업로드에 사용, UploadFile/GetObject 호출')"
      }
    ],
    "concurrency": "고루틴 생성 위치, 채널 방향/버퍼, 뮤텍스/RWMutex 보호 대상, WaitGroup 사용 패턴을 구체적으로 설명. 없으면 '없음'",
    "patterns": "사용된 디자인 패턴(전략/팩토리/옵저버 등) + 적용 위치, 에러 처리 전략(래핑/센티널/커스텀), 리소스 정리 패턴(defer 사용 등). 없으면 '없음'",
    "responsibilities": [
      "이 모듈이 단독으로 책임지는 구체적 기능 항목 (동사+목적어 형식, 예: 'GitHub 설치 토큰 발급 및 캐싱')"
    ],
    "externalDeps": "의존하는 외부 시스템/서비스명, 연동 방식(REST/gRPC/SDK), 인증 방법, 장애 시 영향 범위. 없으면 '없음'",
    "risks": "잠재적 버그 포인트, 동시성 위험, 에러 미처리 경로, 성능 병목 가능성, 보안 취약점. 없으면 '없음'",
    "coupling": "이 모듈이 의존하는 내부 모듈 목록과 각 의존의 이유. 역으로 이 모듈을 의존하는 모듈도 알 수 있으면 포함",
    "testNotes": "이 모듈을 테스트할 때 주의사항: 모킹이 필요한 외부 의존성, 테스트하기 어려운 부분과 이유, 권장 테스트 전략",
    "callFlow": "이 모듈의 전형적인 호출 흐름을 '1. 진입함수() → 2. 내부처리() → 3. 결과반환' 형식으로 단계별 설명. 분기가 있는 경우 주요 분기 경로도 포함",
    "dataTransformations": "이 모듈에서 발생하는 데이터 변환을 '입력타입 → 처리내용 → 출력타입' 형식으로 나열. 파싱, 직렬화, 구조체 매핑, 계산 등 포함. 없으면 '없음'"
  }
]
반드시 위 JSON 배열 형식만 응답하세요. 입력된 모듈 수만큼 배열 원소를 반환하세요.`

	system := "당신은 시니어 소프트웨어 엔지니어이자 코드 분석 전문가입니다. 첨부된 JSON 파일의 각 모듈 코드를 분석하여 정확히 요청된 JSON 배열 형식으로만 응답합니다. 코드에 없는 내용은 추가하지 말고, 있는 내용은 빠짐없이 상세하게 작성하세요."

	return Prompt{User: user, System: system}
}

// ProjectOverviewPrompt는 전체 프로젝트 분석 프롬프트를 생성합니다.
// codeGraph.json, codeContent.json, moduleDetails.json, metrics.json, signals.json 파일이 첨부됩니다.
func ProjectOverviewPrompt(projectMetadataJSON string) Prompt {
	projectMetadataJSON = sanitize(projectMetadataJSON)

	user := fmt.Sprintf(`첨부된 파일들을 분석해주세요:

## 프로젝트 메타데이터
%s

위 프로젝트 제목, 설명, 목표를 우선 기준으로 삼아 코드의 목적과 핵심 흐름을 해석하세요. 코드 구조만 나열하지 말고, 이 프로젝트가 달성하려는 목표와 실제 구현 상태가 어떻게 연결되는지 설명하세요.

## 입력 파일
- codeGraph.json: 코드 구조(노드/엣지/import 관계)
- codeContent.json: 소스코드 내용
- moduleDetails.json: 모듈별 상세 분석 (함수/타입/변수/패턴 포함)
- metrics.json: 정량 메트릭 (모듈 수, 함수 수, 호출 깊이, 순환 의존 등)
- signals.json: 정적 분석 신호 초기값

위 파일들을 종합하여 다음을 JSON으로 응답해주세요:
{
  "analysis": {
    "overview": "프로젝트의 전체 목적과 핵심 기능을 3~5문장으로. 사용된 언어/런타임/주요 프레임워크, 외부 서비스 의존성(DB, 큐, API 등), 배포 환경(서버리스/컨테이너 등)을 구체적으로 포함. 이 프로젝트를 처음 보는 시니어 개발자가 전체 그림을 바로 이해할 수 있도록 작성",
    "architecture": {
      "summary": "전체 아키텍처 스타일(레이어드/헥사고날/이벤트드리븐 등)과 주요 구성 요소를 4~6문장으로 종합 설명. 핵심 설계 결정과 그 이유 포함",
      "layers": "각 계층을 '계층명: 담당 모듈 목록 — 역할 설명' 형식으로 나열. 계층 간 경계와 인터페이스 방식 설명. 계층이 명확하지 않으면 논리적 그룹으로 분류",
      "dependencies": "주요 모듈 간 의존 방향을 'A → B (이유)' 형식으로 나열. metrics.json의 순환 의존 수 기반으로 순환 여부 언급. 결합도가 높은 모듈 쌍 식별",
      "entryPoints": "외부에서 이 시스템으로 진입하는 모든 경로를 구체적으로 열거. 각 진입점의 트리거(HTTP 요청/SQS 메시지/CRON 등), 처리 흐름의 시작점 함수명, 최종 출력 포함"
    },
    "patterns": {
      "concurrency": "프로젝트 전체에서 사용되는 동시성 패턴을 모듈별로 구체적으로 설명. 고루틴 생성 위치, 채널 사용 목적, 뮤텍스 보호 대상, WaitGroup 패턴, 잠재적 경쟁 조건 위치 포함",
      "designPatterns": "식별된 디자인 패턴마다 '패턴명: 적용 위치(모듈/함수명) — 사용 이유' 형식으로 설명. 전략/팩토리/싱글톤/옵저버/데코레이터 등 포함",
      "errorHandling": "에러 처리 전략을 계층별로 분석. 커스텀 에러 타입 사용 여부와 구조, 에러 래핑 패턴, 재시도 가능/불가 구분 방식, 최종 에러 처리 위치 설명",
      "resourceManagement": "파일/DB 연결/네트워크/임시 디렉토리 등 리소스 생성과 해제 패턴. defer 사용 위치, 리소스 누수 가능성 있는 경로, 워크스페이스 정리 로직 설명",
      "security": "인증/인가 메커니즘(토큰, 서명 검증 등), 시크릿/자격증명 관리 방식, 외부 입력 검증 위치, 권한 범위 제어 방식을 구체적으로 설명",
      "externalIntegration": "연동하는 외부 시스템마다 '시스템명: 통신 방식, 인증 방법, 재시도/타임아웃 전략, 실패 시 처리' 형식으로 설명"
    },
    "dataFlow": {
      "initialization": "main 함수부터 각 서비스가 준비 완료되기까지 초기화 순서를 단계별로 나열. 환경변수/설정 로딩 → DB 연결 → 큐 리스너 시작 등 의존 순서 포함",
      "mainWorkflow": "가장 핵심적인 비즈니스 흐름을 '단계 번호. 함수명: 수행 내용' 형식으로 단계별 설명. 각 단계에서 어떤 데이터가 변환/저장/전달되는지 포함",
      "asyncBoundaries": "동기→비동기 전환 경계마다 위치(함수명), 전달 메커니즘(채널/고루틴/큐), 결과 수집 방법, 에러 전파 방식을 설명",
      "dataFormats": "각 시스템 경계(입력 SQS 메시지, DB 모델, AI API 요청/응답, S3 저장 형식, 외부 API 응답 등)에서 사용되는 데이터 구조명과 주요 필드를 구체적으로 설명"
    },
    "qualityIndicators": {
      "strengths": "잘 설계된 부분을 각각 '항목: 구체적 근거 (관련 신호/메트릭 값)' 형식으로 3~5개 설명",
      "risks": "식별된 리스크를 각각 '리스크명: 위치와 증거, 발생 가능한 장애 시나리오' 형식으로 설명. signals.json의 false 항목 중심으로 분석",
      "technicalDebt": "기술 부채 항목마다 '부채 내용: 현재 영향, 개선 방향' 형식으로 설명. 중복 코드, 테스트 부재, 하드코딩된 값, 미완성 구현 등 포함",
      "maintainability": "변경 용이성(모듈 경계 명확도), 확장성(새 기능 추가 시 영향 범위), 가독성(네이밍/구조 일관성), 디버깅 용이성(로깅/에러 메시지 품질) 관점에서 종합 평가"
    },
    "keyDataModels": "시스템에서 가장 중요한 데이터 모델/구조체를 나열하고 관계를 설명. 형식: '구조체명: 용도, 어디서 생성되고 어디로 흐르는지, 연관 구조체'. 시스템 전체를 관통하는 핵심 데이터 흐름의 중심이 되는 타입 우선 설명",
    "moduleInteractions": "모듈 간 주요 상호작용을 나열. 형식: 'A모듈 → B모듈: 호출하는 함수명, 전달하는 데이터 타입, 반환받는 데이터 타입'. 단방향/양방향 의존, 인터페이스를 통한 느슨한 결합 여부도 표시",
    "criticalPaths": "시스템의 핵심 end-to-end 실행 경로를 나열. 형식: '경로명\\n  1. 모듈.함수(): 수행 내용\\n  2. ...'. 정상 경로 외에 에러 경로, 분기 조건도 포함. 가장 자주 실행되거나 장애 시 영향이 큰 경로 우선"
  },
  "signalCorrections": {
    "hasClearModuleBoundaries": true,
    "hasSeparatedOrchestrationLayer": true,
    "hasRuntimeValidation": false,
    "hasTestingIsolationIssue": false,
    "hasRetryConsistencyRisk": false,
    "hasWorkspaceSafetyControls": true,
    "hasErrorWrapping": true,
    "hasGracefulShutdown": false,
    "hasLogging": true,
    "hasAuthentication": true,
    "hasConcurrencyControl": false,
    "hasResourceCleanup": true
  }
}

signalCorrections: 위 예시 값은 무시하고 실제 코드를 분석하여 각 신호를 true/false로 보정하세요.
반드시 위 JSON 형식만 응답하세요.`, projectMetadataJSON)

	system := "당신은 시니어 소프트웨어 아키텍트입니다. 모듈별 분석 결과와 정량 메트릭, 정적 분석 신호를 종합하여 프로젝트 전체를 완전히 이해할 수 있는 상세 문서를 작성합니다. 코드를 보지 않은 시니어 개발자가 이 문서만으로 바로 기여할 수 있는 수준이어야 합니다. 모호한 표현보다 구체적 모듈명/함수명/데이터구조명을 직접 언급하세요. 반드시 요청된 JSON 형식으로만 응답하세요."

	return Prompt{User: user, System: system}
}

// UserViewPrompt는 projectContext 기반으로 사용자용 분석 리포트를 생성하는 프롬프트입니다.
// UserViewPrompt는 projectContext 파일 기반으로 사용자용 분석 리포트를 생성하는 프롬프트입니다.
// projectContext.json 파일은 첨부 파일로 전달됩니다.
func UserViewPrompt(projectMetadataJSON string) Prompt {
	projectMetadataJSON = sanitize(projectMetadataJSON)

	user := fmt.Sprintf(`첨부된 projectContext.json 파일을 분석하여 개발자가 읽기 좋은 리포트를 다음 JSON 형식으로 응답해주세요:

## 프로젝트 메타데이터
%s

프로젝트 설명과 목표를 기준으로 강점, 리스크, 조언을 우선순위화하세요. 단순 코드 품질 평가가 아니라 이 프로젝트 목표 달성에 직접 영향을 주는 내용을 먼저 다루세요.

## 응답 형식
{
  "headline": "프로젝트의 핵심 특징을 담은 한 줄 요약 (30자 이내, 기술스택+아키텍처 특징 중심)",
  "summary": "프로젝트 전체를 2~4문장으로 요약. 주요 기능, 아키텍처 패턴, 강점, 주의 신호 포함",
  "strengths": ["잘 된 점 1", "잘 된 점 2", "..."],
  "risks": ["리스크 1 (신호 근거 포함)", "리스크 2", "..."],
  "advice": [
    {
      "id": "kebab-case-unique-id",
      "priority": "high|medium|low",
      "category": "testing|reliability|security|architecture|performance",
      "title": "조언 제목",
      "body": "상세 설명 (신호/메트릭 근거 포함)",
      "recommendedAction": "구체적 실행 방법",
      "expectedImpact": "기대 효과"
    }
  ],
  "scorecard": {
    "overall": {
      "score": 0~100,
      "grade": "A|B|C|D|F",
      "confidence": "high|medium|low"
    },
    "categories": [
      {
        "key": "ARCHITECTURE|PERFORMANCE_READINESS|CODE_QUALITY|TESTABILITY|RELIABILITY",
        "label": "카테고리 한글 이름",
        "score": 0~100,
        "reason": "점수 근거",
        "evidence": ["신호=값", "메트릭=값"]
      }
    ]
  }
}
반드시 위 JSON 형식만 응답하세요.`, projectMetadataJSON)

	system := "당신은 시니어 소프트웨어 엔지니어이자 기술 멘토입니다. 코드 분석 결과를 바탕으로 개발자가 자신의 프로젝트를 객관적으로 이해하고 성장할 수 있도록 명확하고 실용적인 피드백을 제공합니다. 점수와 등급은 근거 있게, 조언은 구체적으로 작성합니다. 반드시 요청된 JSON 형식으로만 응답하세요."

	return Prompt{User: user, System: system}
}

// QuestPrompt는 퀘스트 평가 + 새 퀘스트 생성 프롬프트를 생성합니다.
// projectContext.json, diff 파일은 첨부 파일로 전달됩니다.
func QuestPrompt(questRequestJSON string) Prompt {
	questRequestJSON = sanitize(questRequestJSON)

	user := fmt.Sprintf(`첨부된 파일을 분석해주세요:
- projectContext.json: 프로젝트 전체 분석 문서
- *.diff (있을 경우): 최근 코드 변경사항

## 퀘스트 요청 정보 (기존 퀘스트 + 최근 평가)
%s

위 정보를 기반으로 다음 두 가지를 수행하고 JSON으로 응답해주세요:

### 1. 기존 퀘스트 평가
각 기존 퀘스트에 대해 diff와 프로젝트 상태를 분석하여 진행 상황을 평가합니다.
- evaluationResult: "NOT_STARTED", "IN_PROGRESS", "ALMOST_DONE", "COMPLETED" 중 하나
- confidenceScore: 0.0 ~ 1.0 사이의 확신도
- reason: 평가 근거를 구체적으로 설명
- progressNote: 다음 평가 시 참고할 진행 메모

### 2. 새 퀘스트 생성
프로젝트 분석 결과와 기존 퀘스트를 고려하여 새로운 개선 퀘스트를 생성합니다.
- project.projectTitle, project.projectDescription, project.projectGoal을 우선 참고하여 프로젝트 목표 달성에 직접 기여하는 퀘스트를 생성할 것
- 기존 퀘스트와 중복되지 않을 것
- 프로젝트의 실제 리스크/개선점에 기반할 것
- 구체적이고 측정 가능한 완료 기준을 제시할 것
- rewardExp: 난이도와 영향도에 비례하는 경험치 (50~500)
- expiredAt: 적절한 마감일 (yyyy-MM-ddTHH:mm:ss 형식)
- roadMapMilestones가 제공된 경우 새 퀘스트마다 가장 관련 있는 milestoneKey를 relatedMilestoneKey에 넣을 것. 직접 연결할 마일스톤이 없으면 빈 문자열

응답 형식:
{
  "questEvaluations": [
    {
      "userAiQuestId": 퀘스트ID,
      "evaluationResult": "IN_PROGRESS",
      "confidenceScore": 0.85,
      "reason": "평가 근거",
      "progressNote": "진행 메모"
    }
  ],
  "newQuests": [
    {
      "title": "퀘스트 제목",
      "description": "퀘스트 설명",
      "hint": "힌트",
      "aiGenerationReason": "생성 이유",
      "completionGuide": "완료 기준",
      "category": "ARCHITECTURE|PERFORMANCE_READINESS|CODE_QUALITY|TESTABILITY|RELIABILITY 중 하나",
      "rewardExp": 150,
      "expiredAt": "2026-04-30T23:59:59",
      "relatedMilestoneKey": "milestone-1-2"
    }
  ]
}
반드시 위 JSON 형식만 응답하세요.`, questRequestJSON)

	system := "당신은 시니어 소프트웨어 엔지니어이자 코드 품질 멘토입니다. 프로젝트의 코드 분석 결과와 변경 이력을 기반으로 개발자의 성장을 돕는 퀘스트를 평가하고 생성합니다. 평가는 객관적 근거에 기반하며, 새 퀘스트는 실질적으로 프로젝트 품질을 개선할 수 있는 구체적인 과제여야 합니다. 반드시 요청된 JSON 형식으로만 응답하세요."

	return Prompt{User: user, System: system}
}

// MilestoneEvalPrompt는 현재 커밋 기준으로 PENDING/IN_PROGRESS 마일스톤의 진행 여부를 평가하는 프롬프트를 생성합니다.
// 첨부 파일: projectContext.json, *.diff
func MilestoneEvalPrompt(milestoneRequestJSON string) Prompt {
	milestoneRequestJSON = sanitize(milestoneRequestJSON)

	user := fmt.Sprintf(`첨부된 파일을 분석해주세요:
- projectContext.json: 프로젝트 전체 분석 문서
- *.diff (있을 경우): 최근 코드 변경사항

## 마일스톤 평가 요청
%s

위 마일스톤 목록에 대해 diff와 projectContext를 기반으로 각 마일스톤의 진행 상태를 평가하고 JSON으로 응답해주세요.

### 평가 기준
- evaluationResult: "PENDING" (변화 없음), "IN_PROGRESS" (부분 진행됨), "ACHIEVED" (완료 조건 충족) 중 하나
- confidenceScore: 0.0 ~ 1.0 사이의 확신도
- reason: diff/projectContext에서 근거가 된 코드·파일·변경 내용을 구체적으로 설명
- progressNote: 다음 평가 시 참고할 진행 메모

### 주의사항
- 현재 status가 PENDING인 마일스톤은 IN_PROGRESS 또는 ACHIEVED로만 올릴 수 있습니다.
- 현재 status가 IN_PROGRESS인 마일스톤은 ACHIEVED로만 올릴 수 있습니다.
- diff에서 직접적인 근거를 찾지 못하면 evaluationResult를 현재 status와 동일하게 유지하세요.
- 모든 마일스톤에 대해 빠짐없이 응답하세요.

응답 형식:
{
  "milestoneEvaluations": [
    {
      "projectMilestoneId": 마일스톤ID,
      "evaluationResult": "IN_PROGRESS",
      "confidenceScore": 0.75,
      "reason": "평가 근거",
      "progressNote": "진행 메모"
    }
  ]
}
반드시 위 JSON 형식만 응답하세요.`, milestoneRequestJSON)

	system := "당신은 시니어 소프트웨어 엔지니어입니다. git diff와 프로젝트 분석 문서를 기반으로 각 로드맵 마일스톤의 실제 진행 상태를 객관적으로 평가합니다. 직접적인 코드 근거 없이 추측하지 않으며, 반드시 요청된 JSON 형식으로만 응답합니다."

	return Prompt{User: user, System: system}
}

// RoadMapPrompt는 projectContext 기반으로 /{projectId}/road-map 조회 API의 원천 데이터를 생성합니다.
// projectContext.json 파일은 첨부 파일로 전달됩니다.
func RoadMapPrompt(projectMetadataJSON string) Prompt {
	projectMetadataJSON = sanitize(projectMetadataJSON)

	user := fmt.Sprintf(`첨부된 projectContext.json을 분석해서 프로젝트 로드맵을 생성해주세요.

## 프로젝트 메타데이터
%s

## 수량 기준 (절대 하한 — 반드시 준수)
- phase: 최소 8개, 권장 8~12개
- milestone: phase당 최소 4개, 권장 4~7개
- 전체 milestone 합계: 최소 40개
- 응답 JSON을 출력하기 전에 phases.length와 milestones.length를 직접 세어 위 기준을 충족하는지 확인하세요. 미달이면 추가하고 나서 출력하세요.

## 생성 원칙
- 이 로드맵은 /{projectId}/road-map API의 phases, milestones 응답을 만들기 위한 DB 원천 데이터입니다.
- projectTitle, projectDescription, projectGoal을 우선 참고하여 프로젝트가 달성하려는 목표 중심으로 phase와 milestone을 설계하세요.
- 현재 리포지토리는 이미 진행 중인 프로젝트일 수 있습니다. 구현된 기능과 코드 증거가 명확한 단계는 COMPLETED/ACHIEVED, 현재 개발 중으로 보이는 단계는 IN_PROGRESS, 미래 작업은 NOT_STARTED/PENDING으로 분류하세요.
- 팀원이 내일 당장 실행할 수 있는 작업형 로드맵으로 작성하세요. 보고서 요약 문장 금지.
- 추상 표현 금지 예시: "기능 고도화", "성능 개선", "안정화", "사용자 경험 향상", "전반적인 최적화" → 반드시 대상(API 경로, 테이블명, 화면명, 컴포넌트명)을 붙여 구체화하세요.
  - 나쁜 예: "인증 기능 개선" / 좋은 예: "POST /auth/refresh 토큰 갱신 엔드포인트 구현 및 RTK 저장 로직 연결"
  - 나쁜 예: "DB 최적화" / 좋은 예: "user_ai_quest 테이블에 (project_id, progress_status) 복합 인덱스 추가"
- 이미 구현된 부분도 "완료됨" 한 줄로 뭉뚱그리지 마세요. 구현된 하위 기능을 milestone 여러 개로 쪼개어 ACHIEVED/IN_PROGRESS/PENDING 상태를 섞어 표현하세요.
- phase는 개발 흐름의 큰 묶음이어야 합니다. 예시: 도메인 모델 정의, 인증/권한, 핵심 API, 분석 파이프라인, 게이미피케이션, 알림/비동기, 관측성, 테스트/QA, 배포/운영, 후속 기능.
- milestone은 하나의 검증 가능한 작업 단위여야 합니다. 화면, API endpoint, DB 스키마, worker 단계, SQS 계약, 롤백 경로, 테스트 케이스처럼 검증 대상이 명확해야 합니다.
- phaseName과 milestoneName은 짧고 행동 지향적인 이름으로 쓰세요. 예: "Analysis job dispatch contract", "Quest category FK binding", "Roadmap phase count validation".
- milestoneIntent는 왜 필요한지, triggerCondition은 언제 시작/검증되는지, expectedState는 완료 후 관찰 가능한 결과를 구체적으로 쓰세요.
- completionRule은 체크리스트처럼 측정 가능해야 합니다. 예: "POST /quests 호출 시 category=CODE_QUALITY가 DB에 저장되고, 잘못된 값은 FK 오류로 거부된다."
- observableEvidence는 코드/DB/API/테스트/로그에서 확인할 수 있는 증거 2~4개를 넣으세요.
- phaseScope는 프론트에서 그대로 보여도 이해 가능한 구체적인 기능 범위 4~6개를 넣으세요.
- phaseKey와 milestoneKey는 영문 kebab-case로 고유하게 생성하세요.
- phase status는 "NOT_STARTED", "IN_PROGRESS", "COMPLETED", "FAILED" 중 하나만 사용하세요.
- milestone status는 "PENDING", "IN_PROGRESS", "ACHIEVED", "FAILED" 중 하나만 사용하세요.
- id, phaseId, milestoneIds, overallProgress는 DB/API가 계산하므로 응답하지 마세요.

응답 형식:
{
  "phases": [
    {
      "phaseKey": "phase-1-foundation",
      "phaseOrder": 1,
      "phaseName": "Foundation",
      "phaseObjective": "이 phase의 목표",
      "phaseOutcome": "완료 후 기대 결과",
      "phaseScope": ["범위 1", "범위 2"],
      "exitCriteria": "phase 종료 기준",
      "status": "IN_PROGRESS"
    }
  ],
  "milestones": [
    {
      "milestoneKey": "milestone-auth-session",
      "phaseKey": "phase-1-foundation",
      "milestoneName": "Auth session flow",
      "milestoneIntent": "이 마일스톤의 의도",
      "triggerCondition": "시작 또는 평가 트리거",
      "expectedState": "완료 후 관찰 가능한 상태",
      "observableEvidence": ["증거 1", "증거 2"],
      "completionRule": "완료 판정 규칙",
      "status": "IN_PROGRESS"
    }
  ]
}
반드시 JSON 객체만 응답하세요.`, projectMetadataJSON)

	system := "당신은 시니어 소프트웨어 엔지니어이자 실무형 프로젝트 로드맵 설계자입니다. 코드 분석 문서를 근거로 실제 구현 상태와 앞으로의 작업을 구분하되, 보고서식 추상 문장이 아니라 개발자가 바로 실행하고 검증할 수 있는 세부 작업 단위의 로드맵을 JSON으로만 작성합니다."

	return Prompt{User: user, System: system}
}

// IncrementalProjectContextPrompt는 기존 ProjectContext를 git diff 기반으로 증분 업데이트하는 프롬프트를 생성합니다.
// 첨부 파일: [baselineProjectContext.json, diff.patch, focusedCodeGraph.json (optional)]
func IncrementalProjectContextPrompt(effectiveBeforeCommit, afterCommit string, projectMetadataJSON string) Prompt {
	projectMetadataJSON = sanitize(projectMetadataJSON)

	user := fmt.Sprintf(`다음 파일들을 참고하여 프로젝트 분석 문서(ProjectContext)를 증분 업데이트하세요.

## 프로젝트 메타데이터
%s

프로젝트 제목, 설명, 목표는 변경 후 분석에서도 유지되어야 합니다. diff 해석과 리스크 판단은 이 프로젝트 목표에 미치는 영향 기준으로 우선순위를 정하세요.

## 입력 파일
1. baseline-project-context.json — 이전 전체 분석 문서 (기준선)
2. changes.diff — beforeCommit(%s) → afterCommit(%s) 사이의 git unified diff
3. focused-code-graph.json — diff에서 변경된 파일에 집중한 코드 그래프 (없을 수 있음)

## 업데이트 원칙
- diff와 focused-code-graph를 기반으로 변경 영향이 있는 부분만 정밀하게 갱신한다
- 변경되지 않은 모듈의 moduleDetails는 baseline 값을 그대로 유지한다
- 변경된 파일이 속한 모듈의 moduleDetails만 재분석하여 교체한다
- metrics는 focused-code-graph의 신규 노드/엣지를 반영하여 재계산한다
- signals는 변경 영향을 반영하여 갱신한다
- analysis(overview, architecture, patterns, dataFlow, qualityIndicators)는 diff 영향 범위에 한해 갱신한다

## 응답 형식 (ProjectContext JSON 전체를 반환)
{
  "metrics": { ... },
  "signals": { ... },
  "analysis": {
    "overview": "...",
    "architecture": { "summary": "...", "layers": "...", "dependencies": "...", "entryPoints": "..." },
    "patterns": { "concurrency": "...", "designPatterns": "...", "errorHandling": "...", "resourceManagement": "...", "security": "...", "externalIntegration": "..." },
    "dataFlow": { "initialization": "...", "mainWorkflow": "...", "asyncBoundaries": "...", "dataFormats": "..." },
    "qualityIndicators": { "strengths": "...", "risks": "...", "technicalDebt": "...", "maintainability": "..." }
  },
  "moduleDetails": [ { "module": "...", "language": "...", "summary": "...", ... } ],
  "codeGraph": { ... },
  "generatedAt": "..."
}
반드시 위 JSON 형식만 응답하세요.`, projectMetadataJSON, effectiveBeforeCommit, afterCommit)

	system := "당신은 시니어 소프트웨어 엔지니어이자 코드 분석 전문가입니다. 기존 프로젝트 분석 문서를 git diff와 변경 파일 코드 그래프를 기반으로 증분 업데이트합니다. 변경되지 않은 부분은 baseline을 그대로 유지하고, 변경된 부분만 정밀하게 갱신합니다. 반드시 요청된 JSON 형식으로만 응답하세요."

	return Prompt{User: user, System: system}
}
