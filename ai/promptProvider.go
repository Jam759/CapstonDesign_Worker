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

각 모듈에 대해 다음을 JSON 배열로 응답해주세요:
[
  {
    "module": "모듈명",
    "summary": "모듈의 역할, 목적, 책임 범위를 상세히 설명",
    "functions": "모든 함수/메서드에 대해 각각의 역할, 파라미터 의미, 반환값, 호출 흐름, 사이드 이펙트를 상세히 설명",
    "types": "모든 클래스/구조체/인터페이스/열거형에 대해 각 필드/속성의 용도, 타입 간 관계, 구현체 목록, 상속/구현 관계, 설계 의도를 상세히 설명",
    "variables": "모든 변수/상수/프로퍼티에 대해 용도, 초기화 시점, 변경 조건, 스코프, 다른 코드와의 관계를 상세히 설명",
    "imports": "각 외부 의존성/라이브러리가 어떤 목적으로 사용되는지, 어떤 함수에서 사용하는지 상세히 설명",
    "concurrency": "비동기/동시성 패턴 사용 여부와 구체적인 사용 방식. 없으면 '없음'",
    "patterns": "디자인 패턴, 에러/예외 처리 패턴, 리소스 관리 패턴 등. 없으면 '없음'",
    "responsibilities": "이 모듈이 담당하는 구체적 책임을 목록으로 설명",
    "externalDeps": "의존하는 외부 시스템/라이브러리와 연동 방식. 없으면 '없음'",
    "risks": "이 모듈 고유의 리스크, 취약점, 개선 필요 사항. 없으면 '없음'"
  }
]
반드시 위 JSON 배열 형식만 응답하세요. 입력된 모듈 수만큼 배열 원소를 반환하세요.`

	system := "당신은 시니어 소프트웨어 엔지니어이자 코드 분석 전문가입니다. 첨부된 JSON 파일의 각 모듈 코드를 분석하여 JSON 배열로 응답합니다. 반드시 요청된 JSON 배열 형식으로만 응답하세요."

	return Prompt{User: user, System: system}
}

// ProjectOverviewPrompt는 전체 프로젝트 분석 프롬프트를 생성합니다.
// codeGraph.json, codeContent.json, moduleDetails.json, metrics.json, signals.json 파일이 첨부됩니다.
func ProjectOverviewPrompt() Prompt {
	user := `첨부된 파일들을 분석해주세요:
- codeGraph.json: 코드 구조(노드/엣지/import 관계)
- codeContent.json: 소스코드 내용
- moduleDetails.json: 사전 계산된 모듈별 분석 요약
- metrics.json: 정량 메트릭
- signals.json: 정적 분석 신호 (초기값)

위 파일들을 종합하여 다음을 JSON으로 응답해주세요:
{
  "analysis": {
    "overview": "프로젝트의 전체 목적, 주요 기능, 사용된 언어/프레임워크/기술 스택, 대상 사용자/환경을 포함한 종합 설명. 최대한 상세하게.",
    "architecture": {
      "summary": "프로젝트의 전체 아키텍처를 종합적으로 설명",
      "layers": "각 계층(진입점, 오케스트레이션, 비즈니스, 데이터 접근 등)의 역할과 경계를 상세히 설명",
      "dependencies": "모듈 간 의존 방향, 순환 여부, 결합도를 메트릭 기반으로 분석",
      "entryPoints": "외부에서 진입하는 모든 경로(API, 웹훅, 큐 리스너, CLI 등)를 상세히 설명"
    },
    "patterns": {
      "concurrency": "프로젝트 전체의 동시성/비동기 패턴을 종합적으로 설명",
      "designPatterns": "사용된 모든 디자인 패턴의 위치와 목적을 상세히 설명",
      "errorHandling": "에러/예외 처리 전략을 전체적으로 분석",
      "resourceManagement": "리소스 생성/해제 패턴을 상세히 설명",
      "security": "인증, 인가, 서명 검증, 시크릿 관리 방식을 상세히 설명",
      "externalIntegration": "외부 API/DB/큐 통신 방식, 인증 흐름, 재시도, 타임아웃을 상세히 설명"
    },
    "dataFlow": {
      "initialization": "앱 시작 시 초기화 순서와 의존성을 상세히 설명",
      "mainWorkflow": "핵심 비즈니스 흐름을 단계별로 상세히 설명",
      "asyncBoundaries": "비동기 경계 위치와 데이터 전달 방식을 상세히 설명",
      "dataFormats": "각 경계에서 사용되는 데이터 형태를 상세히 설명"
    },
    "qualityIndicators": {
      "strengths": "프로젝트의 잘 된 점들을 구체적 근거(메트릭/신호)와 함께 설명",
      "risks": "식별된 리스크들을 신호 기반 근거와 함께 설명",
      "technicalDebt": "기술 부채 요소들을 구체적으로 설명",
      "maintainability": "유지보수성을 변경 용이성, 확장성, 가독성 관점에서 평가"
    }
  },
  "signalCorrections": {
    "hasClearModuleBoundaries": true/false,
    "hasSeparatedOrchestrationLayer": true/false,
    "hasRuntimeValidation": true/false,
    "hasTestingIsolationIssue": true/false,
    "hasRetryConsistencyRisk": true/false,
    "hasWorkspaceSafetyControls": true/false,
    "hasErrorWrapping": true/false,
    "hasGracefulShutdown": true/false,
    "hasLogging": true/false,
    "hasAuthentication": true/false,
    "hasConcurrencyControl": true/false,
    "hasResourceCleanup": true/false
  }
}

signalCorrections: 정적 분석 초기값을 코드 내용 기반으로 재평가하여 보정한 값입니다. 정적 분석이 놓친 부분이나 오탐을 교정해주세요.
반드시 위 JSON 형식만 응답하세요. 각 항목은 최대한 상세하게 작성하세요.`

	system := "당신은 시니어 소프트웨어 아키텍트입니다. 모듈별 분석 결과와 정량 메트릭, 정적 분석 신호를 종합하여 프로젝트 전체를 완전히 이해할 수 있도록 상세한 문서를 작성합니다. 코드를 직접 보지 않은 개발자도 이 프로젝트에 바로 기여할 수 있을 수준의 상세함이 필요합니다. signalCorrections에서는 정적 분석 초기값을 코드 맥락을 고려하여 정확하게 보정합니다. 반드시 요청된 JSON 형식으로만 응답하세요."

	return Prompt{User: user, System: system}
}

// UserViewPrompt는 projectContext 기반으로 사용자용 분석 리포트를 생성하는 프롬프트입니다.
// UserViewPrompt는 projectContext 파일 기반으로 사용자용 분석 리포트를 생성하는 프롬프트입니다.
// projectContext.json 파일은 첨부 파일로 전달됩니다.
func UserViewPrompt() Prompt {
	user := `첨부된 projectContext.json 파일을 분석하여 개발자가 읽기 좋은 리포트를 다음 JSON 형식으로 응답해주세요:
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
        "key": "architecture|performance_readiness|code_quality|testability|reliability",
        "label": "카테고리 한글 이름",
        "score": 0~100,
        "reason": "점수 근거",
        "evidence": ["신호=값", "메트릭=값"]
      }
    ]
  }
}
반드시 위 JSON 형식만 응답하세요.`

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
- 기존 퀘스트와 중복되지 않을 것
- 프로젝트의 실제 리스크/개선점에 기반할 것
- 구체적이고 측정 가능한 완료 기준을 제시할 것
- rewardExp: 난이도와 영향도에 비례하는 경험치 (50~500)
- expiredAt: 적절한 마감일 (yyyy-MM-ddTHH:mm:ss 형식)

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
      "rewardExp": 150,
      "expiredAt": "2026-04-30T23:59:59"
    }
  ]
}
반드시 위 JSON 형식만 응답하세요.`, questRequestJSON)

	system := "당신은 시니어 소프트웨어 엔지니어이자 코드 품질 멘토입니다. 프로젝트의 코드 분석 결과와 변경 이력을 기반으로 개발자의 성장을 돕는 퀘스트를 평가하고 생성합니다. 평가는 객관적 근거에 기반하며, 새 퀘스트는 실질적으로 프로젝트 품질을 개선할 수 있는 구체적인 과제여야 합니다. 반드시 요청된 JSON 형식으로만 응답하세요."

	return Prompt{User: user, System: system}
}

// IncrementalProjectContextPrompt는 기존 ProjectContext를 git diff 기반으로 증분 업데이트하는 프롬프트를 생성합니다.
// 첨부 파일: [baselineProjectContext.json, diff.patch, focusedCodeGraph.json (optional)]
func IncrementalProjectContextPrompt(effectiveBeforeCommit, afterCommit string) Prompt {
	user := fmt.Sprintf(`다음 파일들을 참고하여 프로젝트 분석 문서(ProjectContext)를 증분 업데이트하세요.

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
반드시 위 JSON 형식만 응답하세요.`, effectiveBeforeCommit, afterCommit)

	system := "당신은 시니어 소프트웨어 엔지니어이자 코드 분석 전문가입니다. 기존 프로젝트 분석 문서를 git diff와 변경 파일 코드 그래프를 기반으로 증분 업데이트합니다. 변경되지 않은 부분은 baseline을 그대로 유지하고, 변경된 부분만 정밀하게 갱신합니다. 반드시 요청된 JSON 형식으로만 응답하세요."

	return Prompt{User: user, System: system}
}
