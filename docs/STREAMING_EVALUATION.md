# Streaming evaluation: stiction vs. non-stiction, classic detector vs. RF

Quantifies what `V3_PLAN.md` and `E2E_TEST.md` described qualitatively ("the RF model doesn't generalize to the simulator's synthetic signal") with a real streaming run and standard classification metrics, computed against a large enough sample to be more than an anecdote.

> **Update**: the RF model's live-streaming failure documented below was fixed via synthetic-data domain-randomization augmentation in [valve-stiction-ml](https://github.com/maulanaiskak/valve-stiction-ml) (see its `ML_PLAN.md` §15 for the full methodology) — a re-run of this exact evaluation now scores **AUC 0.9998, 99.6% accuracy, F1 0.996**, up from AUC 0.079. See "Update: after retraining" at the bottom. The original run is kept below as-is — it's the evidence the fix was actually needed, not a stale result to delete.

## Setup

6 [valve-stiction-simulator](https://github.com/maulanaiskak/valve-stiction-simulator) instances streaming concurrently (0.02s sample interval — a genuine streaming load, not single-shot), 3 with `STICTION_ENABLED=true` (`stiction-1/2/3`), 3 with `STICTION_ENABLED=false` (`healthy-1/2/3`), through the full pipeline: Mosquitto → [ingestion](https://github.com/maulanaiskak/valve-stiction-ingestion) (window size 100, stride 25) → [detection](https://github.com/maulanaiskak/valve-stiction-detection) (gRPC, classic detector + RF) → TimescaleDB. All 5 repos' current `main` branch, built and run standalone exactly as in `E2E_TEST.md` (separate Docker network, no monorepo, no orchestrator).

Ground truth is the simulator's own `STICTION_ENABLED` flag — not a third detector's opinion.

**N = 267 windows** (131 healthy, 136 stiction) accumulated over several minutes of continuous streaming before evaluation.

## Results

### Classic detector (ellipse-fit + Kano pattern)

| | Predicted: no | Predicted: yes |
|---|---|---|
| **Actual: healthy** | 131 (TN) | 0 (FP) |
| **Actual: stiction** | 0 (FN) | 136 (TP) |

**Accuracy = 100.0% · Precision = 100.0% · Recall = 100.0% · F1 = 1.000**

Zero misclassifications across 267 windows and 6 concurrently-streaming sensors — the training-free detector separates the two classes perfectly on this signal, consistent with every prior check in this project (V1's load test, the simulator's own regression test).

### RF model (valve-stiction-ml, trained on ISDB/SACAC)

| | Predicted: no | Predicted: yes |
|---|---|---|
| **Actual: healthy** | 0 (TN) | 131 (FP) |
| **Actual: stiction** | 0 (FN) | 136 (TP) |

**Accuracy = 50.9% · Precision = 50.9% · Recall = 100.0% · F1 = 0.675 · AUC = 0.079**

The RF predicted "yes" for every single window, healthy or not — accuracy is just the dataset's positive rate. F1 (0.675) looks deceptively reasonable in isolation; it's an artifact of always predicting the positive class against a roughly balanced dataset, not evidence of discrimination. **AUC = 0.079 is the metric that actually exposes it**: computed by ranking windows by `rf_probability`, an AUC this far below 0.5 means the model's confidence score is *inversely* correlated with ground truth here — on average it was more confident about the healthy sensors than the stiction ones (mean `rf_probability`: healthy ≈ 0.89–0.96, stiction ≈ 0.88–0.89, per-sensor breakdown below).

### Per-sensor breakdown

| sensor_id | ground truth | n | classic → yes | RF → yes |
|---|---|---|---|---|
| healthy-1 | healthy | 44 | 0 | 44 |
| healthy-2 | healthy | 44 | 0 | 44 |
| healthy-3 | healthy | 43 | 0 | 43 |
| stiction-1 | stiction | 46 | 46 | 46 |
| stiction-2 | stiction | 45 | 45 | 45 |
| stiction-3 | stiction | 45 | 45 | 45 |

Perfect per-sensor consistency for both detectors — this isn't noisy or borderline, both are fully deterministic given this signal (classic: always right; RF: always predicts "yes" regardless of input).

## Reading

This isn't "the RF model is bad." It's a textbook train/serve distribution shift: the RF was trained on real industrial process data (ISDB/SACAC) and is being evaluated here against a structurally different signal (a scripted triangle-wave OP + stick-slip valve model) it never saw during training. `predict_proba` is doing exactly what a distance-based ensemble does when handed inputs far outside its training distribution — producing confident-looking numbers that don't track anything meaningful. The classic detector has no such failure mode because it isn't fit to a training distribution at all; it's a fixed geometric rule.

**What this evaluation is not**: a claim that the RF model is broken in general — it scored ROC-AUC 0.865 / PR-AUC 0.788 on real held-out SACAC data (see [valve-stiction-ml](https://github.com/maulanaiskak/valve-stiction-ml)'s `ML_PLAN.md`). It's a demonstration that a model's offline validation metrics don't transfer to a materially different serving-time input distribution — worth having in a portfolio specifically because it's an honest negative result, not manufactured.

**What this means for the system design**: NFR-1 in `HLD.md` ("availability of the classic detector under model drift") exists because of exactly this failure mode, discovered empirically rather than assumed — the dashboard shows both labels precisely so a disagreement like this one is visible to whoever's looking at it, not silently averaged away.

## Update: after retraining

The same evaluation setup (6 simulators, 3 stiction / 3 healthy, streaming through the same 5 independently-built repos) re-run against the retrained model, in a fresh set of containers:

| | Before | After (iteration 1) | **After (iteration 2, final)** |
|---|---|---|---|
| Accuracy | 50.9% | 87% | **99.6%** |
| Precision | 50.9% | 74.3% | **99.2%** |
| Recall | 100.0% | 100.0% | **100.0%** |
| F1 | 0.675 | 0.885 | **0.996** |
| AUC | 0.079 | 0.996 | **0.9998** |

N = 255 windows for the final run (1 false positive total, out of 130 healthy windows). Classic detector on the same run: 99.2% accuracy, F1 0.992 — the two detectors are now essentially in agreement, both close to perfect on this signal.

Two iterations were needed, not one:

- **Iteration 1** (synthetic training data with an unconstrained parameter regime) got live-streaming AUC to 0.996 but accuracy only to 87% — the model learned to *rank* windows correctly but the decision threshold (still near 0.5) was miscalibrated for this signal's probability distribution. Fixed by deploying the threshold that F1-maximizes on training-time cross-validated predictions (0.555) instead of the default 0.5 — chosen without looking at this streaming data or the synthetic held-out set, exactly like every other threshold decision in this project's methodology.
- **Iteration 2** (tightened synthetic parameter sampling — `stick_band`/`noise_std` scaled to `amplitude`, the same well-behaved regime the simulator's own default config sits in, after a unit test caught that independent sampling produced some configs whose *signal* didn't actually match its ground-truth label) closed the rest of the gap: synthetic held-out F1/AUC both hit 1.000, and this live-streaming re-run followed at 99.6% accuracy.

Full reasoning, the label-quality bug the test suite caught, and the complete before/after numbers for SACAC (real, untouched) and the synthetic held-out set are in [valve-stiction-ml](https://github.com/maulanaiskak/valve-stiction-ml)'s `ML_PLAN.md` §15 — this file stays focused on the live-pipeline evaluation, that one owns the training methodology.

This doesn't retire NFR-1. The fix closes the gap for the family of signals this project actually generates and streams; a materially different live signal could still expose a similar mismatch. The classic detector keeps running unconditionally regardless.
