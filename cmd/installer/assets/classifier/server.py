import json
import os
import time
from pathlib import Path
from typing import Any, Dict, List, Optional

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field
from transformers import AutoModel, AutoTokenizer, pipeline


DEFAULT_MODEL_DIR = r"C:\ProgramData\Themisto\models\deberta"
DEFAULT_LABELS_PATH = r"C:\Program Files\Themisto\classifier\labels.json"


class SemanticRequest(BaseModel):
    prompt_text: str = Field(default="")
    policy: str = Field(default="")
    surface: str = Field(default="")
    app_name: str = Field(default="")
    destination_host: str = Field(default="")
    destination_path: str = Field(default="")
    vendor: str = Field(default="")
    service_category: str = Field(default="")
    dlp: Optional[Dict[str, Any]] = None
    metadata: Dict[str, str] = Field(default_factory=dict)


class SemanticResult(BaseModel):
    decision: str
    confidence: float
    reason: str
    category: str
    source: str = "local_deberta"
    ambiguous: bool
    latency_ms: int


def _load_labels(path: str) -> Dict[str, Any]:
    labels_path = Path(path)
    if not labels_path.exists():
        labels_path = Path(__file__).with_name("labels.json")
    with labels_path.open("r", encoding="utf-8") as fh:
        data = json.load(fh)
    if not data.get("candidate_labels"):
        raise RuntimeError("labels.json must define candidate_labels")
    return data


class DebertaClassifier:
    def __init__(self) -> None:
        model_dir = os.environ.get("THEMISTO_SEMANTIC_MODEL_DIR", DEFAULT_MODEL_DIR)
        labels_path = os.environ.get("THEMISTO_SEMANTIC_LABELS", DEFAULT_LABELS_PATH)
        self.labels = _load_labels(labels_path)
        self.model_dir = model_dir
        self.model_mode = os.environ.get(
            "THEMISTO_SEMANTIC_MODEL_MODE",
            str(self.labels.get("model_mode", "zero_shot_nli")),
        )
        self.candidate_labels: List[str] = list(self.labels["candidate_labels"])
        self.block_labels = set(self.labels.get("block_labels", [self.labels.get("block_label", "corporate data risk")]))
        self.alert_labels = set(self.labels.get("alert_labels", []))
        self.allow_labels = set(self.labels.get("allow_labels", [self.labels.get("allow_label", "approved business AI use")]))
        self.ambiguous_labels = set(self.labels.get("ambiguous_labels", [self.labels.get("ambiguous_label", "ambiguous corporate AI request")]))
        self.category_map: Dict[str, str] = dict(self.labels.get("category_map", {}))
        self.block_threshold = float(self.labels.get("block_threshold", 0.68))
        self.allow_threshold = float(self.labels.get("allow_threshold", 0.58))
        self.alert_threshold = float(self.labels.get("alert_threshold", 0.55))
        self.hypothesis_template = str(
            self.labels.get("hypothesis_template", "This workplace AI prompt is about {}.")
        )
        self.block_terms: List[str] = [
            str(term).lower() for term in self.labels.get("block_terms", [])
        ]
        self.alert_terms: List[str] = [
            str(term).lower() for term in self.labels.get("alert_terms", [])
        ]
        self.allow_terms: List[str] = [
            str(term).lower() for term in self.labels.get("allow_terms", [])
        ]

        model_path = Path(model_dir)
        if not model_path.exists():
            raise RuntimeError(f"DeBERTa model directory does not exist: {model_dir}")

        if self.model_mode == "zero_shot_nli":
            self.pipe = pipeline(
                "zero-shot-classification",
                model=str(model_path),
                tokenizer=str(model_path),
                device=-1,
            )
            self.tokenizer = None
            self.model = None
        else:
            self.pipe = None
            self.tokenizer = AutoTokenizer.from_pretrained(str(model_path))
            self.model = AutoModel.from_pretrained(str(model_path))

    def classify(self, req: SemanticRequest) -> SemanticResult:
        start = time.perf_counter()
        text = req.prompt_text.strip()
        if not text:
            raise HTTPException(status_code=400, detail="prompt_text is required")

        if self._has_deterministic_dlp_signal(req):
            return SemanticResult(
                decision="block",
                confidence=0.99,
                reason="Themisto's deterministic DLP scanner found credentials, source code, regulated data, or another protected value.",
                category="deterministic_dlp_match",
                source="local_dlp_guard",
                ambiguous=False,
                latency_ms=int((time.perf_counter() - start) * 1000),
            )

        if self.pipe is None:
            return self._classify_with_policy_scorer(req, start)

        result = self.pipe(
            text,
            candidate_labels=self.candidate_labels,
            hypothesis_template=self.hypothesis_template,
            multi_label=False,
        )
        labels = [str(label) for label in result.get("labels", [])]
        scores = [float(score) for score in result.get("scores", [])]
        if not labels or not scores:
            raise RuntimeError("classifier returned no labels")

        top_label = labels[0]
        confidence = max(0.0, min(1.0, scores[0]))
        category = self.category_map.get(top_label, "ambiguous_corporate_ai_request")

        if top_label in self.block_labels and confidence >= self.block_threshold:
            decision = "block"
            reason = "The NLI model found a high-confidence corporate data or AI policy risk."
            ambiguous = False
        elif top_label in self.allow_labels and confidence >= self.allow_threshold:
            decision = "forward"
            reason = "The prompt appears consistent with approved business AI use."
            ambiguous = False
        elif top_label in self.alert_labels and confidence >= self.alert_threshold:
            decision = "alert"
            reason = "The NLI model found a potential policy risk that should be recorded for review."
            ambiguous = False
        else:
            decision = "alert"
            category = "ambiguous_corporate_ai_request"
            reason = "The local NLI classifier is uncertain and should be reviewed by the gateway fallback."
            ambiguous = True

        return SemanticResult(
            decision=decision,
            confidence=confidence,
            reason=reason,
            category=category,
            source="local_deberta_nli",
            ambiguous=ambiguous,
            latency_ms=int((time.perf_counter() - start) * 1000),
        )

    @staticmethod
    def _has_deterministic_dlp_signal(req: SemanticRequest) -> bool:
        dlp_payload = req.dlp or {}
        return bool(
            dlp_payload.get("has_matches")
            or dlp_payload.get("match_count")
            or dlp_payload.get("contains_credentials")
            or dlp_payload.get("contains_source_code")
            or dlp_payload.get("contains_pii")
        )

    def _classify_with_policy_scorer(self, req: SemanticRequest, start: float) -> SemanticResult:
        combined = " ".join(
            part for part in [
                req.prompt_text,
                req.policy,
                req.surface,
                req.app_name,
                req.destination_host,
                req.vendor,
                req.service_category,
                json.dumps(req.dlp or {}, sort_keys=True),
            ]
            if part
        ).lower()

        block_hits = [term for term in self.block_terms if term and term in combined]
        alert_hits = [term for term in self.alert_terms if term and term in combined]
        allow_hits = [term for term in self.allow_terms if term and term in combined]

        if len(block_hits) >= 2:
            reason = "The prompt appears to include sensitive data, credentials, source code, regulated records, or explicit exfiltration intent."
            return SemanticResult(
                decision="block",
                confidence=0.82,
                reason=reason,
                category="corporate_data_risk",
                source="local_deberta_policy_scorer",
                ambiguous=False,
                latency_ms=int((time.perf_counter() - start) * 1000),
            )

        if block_hits or alert_hits:
            reason = "The prompt may involve unsanctioned AI use or sensitive corporate context and should be reviewed by the gateway fallback."
            return SemanticResult(
                decision="alert",
                confidence=0.66 if block_hits else 0.6,
                reason=reason,
                category="potential_policy_risk",
                source="local_deberta_policy_scorer",
                ambiguous=True,
                latency_ms=int((time.perf_counter() - start) * 1000),
            )

        if allow_hits:
            return SemanticResult(
                decision="forward",
                confidence=0.62,
                reason="The prompt appears to request ordinary business assistance without sensitive data exposure.",
                category="approved_business_ai",
                source="local_deberta_policy_scorer",
                ambiguous=False,
                latency_ms=int((time.perf_counter() - start) * 1000),
            )

        return SemanticResult(
            decision="alert",
            confidence=0.52,
            reason="The base DeBERTa model is loaded, but this prompt needs gateway review because no fine-tuned local classification head is configured.",
            category="ambiguous_corporate_ai_request",
            source="local_deberta_policy_scorer",
            ambiguous=True,
            latency_ms=int((time.perf_counter() - start) * 1000),
        )


app = FastAPI(title="Themisto Labs Local Semantic Classifier", version="0.1.0")
classifier: Optional[DebertaClassifier] = None
startup_error = ""


@app.on_event("startup")
def startup() -> None:
    global classifier, startup_error
    try:
        classifier = DebertaClassifier()
        startup_error = ""
    except Exception as exc:
        classifier = None
        startup_error = str(exc)


@app.get("/healthz")
def healthz() -> Dict[str, Any]:
    return {
        "ok": classifier is not None,
        "source": "local_deberta_nli" if classifier and classifier.model_mode == "zero_shot_nli" else "local_deberta",
        "model_dir": os.environ.get("THEMISTO_SEMANTIC_MODEL_DIR", DEFAULT_MODEL_DIR),
        "model_mode": classifier.model_mode if classifier else os.environ.get("THEMISTO_SEMANTIC_MODEL_MODE", "unknown"),
        "error": startup_error,
    }


@app.post("/v1/classify", response_model=SemanticResult)
def classify(req: SemanticRequest) -> SemanticResult:
    if classifier is None:
        raise HTTPException(status_code=503, detail=startup_error or "classifier not loaded")
    return classifier.classify(req)
