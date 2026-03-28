#!/usr/bin/env python3
from __future__ import annotations

import argparse
import importlib.util
import json
import shutil
import sys
from pathlib import Path


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Train a LoRA adapter for Gopher AI.")
    parser.add_argument("--dataset", required=True)
    parser.add_argument("--output-dir", required=True)
    parser.add_argument("--base-model", required=True)
    parser.add_argument("--epochs", type=int, default=3)
    parser.add_argument("--learning-rate", type=float, default=0.0001)
    parser.add_argument("--batch-size", type=int, default=1)
    parser.add_argument("--max-length", type=int, default=1024)
    parser.add_argument("--gradient-accumulation-steps", type=int, default=4)
    parser.add_argument("--warmup-ratio", type=float, default=0.03)
    parser.add_argument("--lora-r", type=int, default=16)
    parser.add_argument("--lora-alpha", type=int, default=32)
    parser.add_argument("--lora-dropout", type=float, default=0.05)
    parser.add_argument(
        "--target-modules",
        default="q_proj,k_proj,v_proj,o_proj,gate_proj,up_proj,down_proj",
    )
    parser.add_argument("--limit-rows", type=int, default=0)
    return parser.parse_args()


def ensure_dependencies() -> None:
    required = ["torch", "transformers", "peft"]
    missing = [name for name in required if importlib.util.find_spec(name) is None]
    if missing:
        raise RuntimeError(
            "missing python packages: "
            + ", ".join(missing)
            + ". install them with pip before auto training can run"
        )


def load_dataset(dataset_path: Path, limit_rows: int) -> list[dict]:
    rows: list[dict] = []
    with dataset_path.open("r", encoding="utf-8") as handle:
        for line_number, line in enumerate(handle, start=1):
            stripped = line.strip()
            if not stripped:
                continue
            try:
                row = json.loads(stripped)
            except json.JSONDecodeError as exc:
                raise ValueError(f"invalid jsonl on line {line_number}: {exc}") from exc
            instruction = str(row.get("instruction", "")).strip()
            response = str(row.get("response", "")).strip()
            if not instruction or not response:
                raise ValueError(f"dataset row {line_number} is missing instruction or response")
            rows.append({"instruction": instruction, "response": response})
            if limit_rows > 0 and len(rows) >= limit_rows:
                break
    if not rows:
        raise ValueError("dataset is empty")
    return rows


def format_example(row: dict) -> str:
    return f"<s>[INST] {row['instruction']} [/INST] {row['response']}</s>"


def load_runtime():
    import torch
    from peft import LoraConfig, TaskType, get_peft_model
    from transformers import AutoModelForCausalLM, AutoTokenizer, Trainer, TrainingArguments

    return torch, LoraConfig, TaskType, get_peft_model, AutoModelForCausalLM, AutoTokenizer, Trainer, TrainingArguments


def build_dataset(rows: list[dict], tokenizer, max_length: int, torch_module):
    class TrainingDataset(torch_module.utils.data.Dataset):
        def __init__(self) -> None:
            self.items: list[dict] = []
            for row in rows:
                encoded = tokenizer(
                    format_example(row),
                    truncation=True,
                    padding="max_length",
                    max_length=max_length,
                )
                input_ids = encoded["input_ids"]
                attention_mask = encoded["attention_mask"]
                labels = [
                    token if mask == 1 else -100
                    for token, mask in zip(input_ids, attention_mask)
                ]
                self.items.append(
                    {
                        "input_ids": torch_module.tensor(input_ids, dtype=torch_module.long),
                        "attention_mask": torch_module.tensor(attention_mask, dtype=torch_module.long),
                        "labels": torch_module.tensor(labels, dtype=torch_module.long),
                    }
                )

        def __len__(self) -> int:
            return len(self.items)

        def __getitem__(self, index: int) -> dict:
            return self.items[index]

    return TrainingDataset()


def train(args: argparse.Namespace, rows: list[dict]) -> dict:
    torch, LoraConfig, TaskType, get_peft_model, AutoModelForCausalLM, AutoTokenizer, Trainer, TrainingArguments = load_runtime()

    output_dir = Path(args.output_dir)
    output_dir.mkdir(parents=True, exist_ok=True)

    tokenizer = AutoTokenizer.from_pretrained(args.base_model, use_fast=True)
    if tokenizer.pad_token is None:
        tokenizer.pad_token = tokenizer.eos_token or tokenizer.unk_token
    tokenizer.padding_side = "right"

    has_cuda = torch.cuda.is_available()
    use_bf16 = has_cuda and hasattr(torch.cuda, "is_bf16_supported") and torch.cuda.is_bf16_supported()
    dtype = torch.bfloat16 if use_bf16 else (torch.float16 if has_cuda else torch.float32)

    model_kwargs = {"torch_dtype": dtype}
    if has_cuda:
        model_kwargs["device_map"] = "auto"

    model = AutoModelForCausalLM.from_pretrained(args.base_model, **model_kwargs)
    model.config.use_cache = False
    if hasattr(model, "gradient_checkpointing_enable"):
        model.gradient_checkpointing_enable()

    target_modules = [part.strip() for part in args.target_modules.split(",") if part.strip()]
    model = get_peft_model(
        model,
        LoraConfig(
            task_type=TaskType.CAUSAL_LM,
            r=args.lora_r,
            lora_alpha=args.lora_alpha,
            lora_dropout=args.lora_dropout,
            bias="none",
            target_modules=target_modules,
        ),
    )

    train_dataset = build_dataset(rows, tokenizer, args.max_length, torch)
    training_args = TrainingArguments(
        output_dir=str(output_dir),
        overwrite_output_dir=True,
        num_train_epochs=args.epochs,
        per_device_train_batch_size=args.batch_size,
        gradient_accumulation_steps=args.gradient_accumulation_steps,
        learning_rate=args.learning_rate,
        warmup_ratio=args.warmup_ratio,
        logging_steps=1,
        logging_first_step=True,
        save_strategy="epoch",
        save_total_limit=1,
        report_to=[],
        remove_unused_columns=False,
        dataloader_pin_memory=has_cuda,
        fp16=has_cuda and not use_bf16,
        bf16=use_bf16,
    )

    trainer = Trainer(
        model=model,
        args=training_args,
        train_dataset=train_dataset,
    )

    result = trainer.train()
    model.save_pretrained(str(output_dir), safe_serialization=True)
    tokenizer.save_pretrained(str(output_dir))

    adapter_source = output_dir / "adapter_model.safetensors"
    adapter_target = output_dir / "adapter.safetensors"
    if adapter_source.exists():
        shutil.copyfile(adapter_source, adapter_target)

    if not adapter_target.exists():
        raise RuntimeError("training finished but adapter.safetensors was not created")

    manifest = {
        "status": "completed",
        "baseModel": args.base_model,
        "dataset": str(Path(args.dataset).resolve()),
        "outputDir": str(output_dir.resolve()),
        "rows": len(rows),
        "hyperparameters": {
            "epochs": args.epochs,
            "learningRate": args.learning_rate,
            "batchSize": args.batch_size,
            "maxLength": args.max_length,
            "gradientAccumulationSteps": args.gradient_accumulation_steps,
            "warmupRatio": args.warmup_ratio,
            "loraR": args.lora_r,
            "loraAlpha": args.lora_alpha,
            "loraDropout": args.lora_dropout,
            "targetModules": target_modules,
        },
        "adapterPath": str(adapter_target.resolve()),
        "loss": float(getattr(result, "training_loss", 0.0) or 0.0),
    }
    manifest_path = output_dir / "training_manifest.json"
    manifest_path.write_text(json.dumps(manifest, indent=2), encoding="utf-8")

    return {
        "status": "completed",
        "manifest": str(manifest_path.resolve()),
        "rows": len(rows),
        "outputDir": str(output_dir.resolve()),
        "adapterPath": str(adapter_target.resolve()),
        "loss": manifest["loss"],
        "accuracy": 0.0,
    }


def main() -> int:
    args = parse_args()
    dataset_path = Path(args.dataset)
    if not dataset_path.exists():
        print(json.dumps({"error": f"dataset not found: {dataset_path}"}), file=sys.stderr)
        return 1

    try:
        ensure_dependencies()
        rows = load_dataset(dataset_path, args.limit_rows)
        result = train(args, rows)
    except Exception as exc:
        print(json.dumps({"error": str(exc)}), file=sys.stderr)
        return 1

    print(json.dumps(result))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
