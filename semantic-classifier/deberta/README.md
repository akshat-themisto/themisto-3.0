# Themisto Labs Local DeBERTa Classifier

This sidecar serves the local semantic classifier on loopback:

`http://127.0.0.1:17177/v1/classify`

It expects a DeBERTa model directory at:

`C:\ProgramData\Themisto\models\deberta`

The installer copies the service files to:

`C:\Program Files\Themisto\classifier`

For development, download a model and run:

```powershell
powershell -ExecutionPolicy Bypass -File .\semantic-classifier\deberta\download-model.ps1
powershell -ExecutionPolicy Bypass -File .\semantic-classifier\deberta\start-semantic-classifier.ps1 `
  -ClassifierDir ".\semantic-classifier\deberta" `
  -ModelDir "C:\ProgramData\Themisto\models\deberta"
```

Then test:

```powershell
Invoke-RestMethod http://127.0.0.1:17177/healthz
```

The default model is
`MoritzLaurer/deberta-v3-xsmall-zeroshot-v1.1-all-33`. It is an NLI-trained
DeBERTa checkpoint sized for local endpoint inference. The corporate policy
statements in `labels.json` are evaluated as zero-shot candidate labels.

Deterministic DLP findings still block first. NLI handles contextual risks and
paraphrases; uncertain results return `alert` for gateway review.
