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

The default download model is `microsoft/deberta-v3-small`.

That model is a normal base DeBERTa model, not a zero-shot/NLI classifier. By
default the sidecar loads the base model for local runtime validation and uses
the corporate policy scorer in `labels.json` for data leakage, credentials,
source code, regulated data, unsafe AI use, and unsanctioned AI indicators. If
you deploy a real zero-shot/NLI or fine-tuned classifier head, set
`THEMISTO_SEMANTIC_MODEL_MODE=zero_shot_nli` and provide matching labels.

For development with an explicit NLI model, pass:

```powershell
powershell -ExecutionPolicy Bypass -File .\semantic-classifier\deberta\download-model.ps1 `
  -ModelId "MoritzLaurer/deberta-v3-base-zeroshot-v2.0"
```
