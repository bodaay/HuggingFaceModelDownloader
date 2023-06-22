---
inference: false
license: other
---

<!-- header start -->
<div style="width: 100%;">
    <img src="https://i.imgur.com/EBdldam.jpg" alt="TheBlokeAI" style="width: 100%; min-width: 400px; display: block; margin: auto;">
</div>
<div style="display: flex; justify-content: space-between; width: 100%;">
    <div style="display: flex; flex-direction: column; align-items: flex-start;">
        <p><a href="https://discord.gg/Jq4vkcDakD">Chat & support: my new Discord server</a></p>
    </div>
    <div style="display: flex; flex-direction: column; align-items: flex-end;">
        <p><a href="https://www.patreon.com/TheBlokeAI">Want to contribute? TheBloke's Patreon page</a></p>
    </div>
</div>
<!-- header end -->

# Tim Dettmers' Guanaco 65B GPTQ

These files are GPTQ 4bit model files for [Tim Dettmers' Guanaco 65B](https://huggingface.co/timdettmers/guanaco-65b).

It is the result of merging the LoRA then quantising to 4bit using [GPTQ-for-LLaMa](https://github.com/qwopqwop200/GPTQ-for-LLaMa).

## Other repositories available

* [4-bit GPTQ models for GPU inference](https://huggingface.co/TheBloke/guanaco-65B-GPTQ)
* [4-bit, 5-bit and 8-bit GGML models for CPU(+GPU) inference](https://huggingface.co/TheBloke/guanaco-65B-GGML)
* [Merged, unquantised fp16 model in HF format](https://huggingface.co/TheBloke/guanaco-65B-HF)

## Prompt template

```
### Human: prompt
### Assistant:
```

## How to easily download and use this model in text-generation-webui

Open the text-generation-webui UI as normal.

1. Click the **Model tab**.
2. Under **Download custom model or LoRA**, enter `TheBloke/guanaco-65B-GPTQ`.
3. Click **Download**.
4. Wait until it says it's finished downloading.
5. Click the **Refresh** icon next to **Model** in the top left.
6. In the **Model drop-down**: choose the model you just downloaded, `guanaco-65B-GPTQ`.
7. If you see an error in the bottom right, ignore it - it's temporary.
8. Fill out the `GPTQ parameters` on the right: `Bits = 4`, `Groupsize = None`, `model_type = Llama`
9. Click **Save settings for this model** in the top right.
10. Click **Reload the Model** in the top right.
11. Once it says it's loaded, click the **Text Generation tab** and enter a prompt!

## Provided files

**Compatible file - Guanaco-65B-GPTQ-4bit.act-order.safetensors**

In the `main` branch you will find `Guanaco-65B-GPTQ-4bit.act-order.safetensors`

This will work with all versions of GPTQ-for-LLaMa. It has maximum compatibility.

It was created without groupsize to minimise VRAM requirements. It was created with the `--act-order` parameter to maximise accuracy.

* `Guanaco-65B-GPTQ-4bit.act-order.safetensors`
  * Works with all versions of GPTQ-for-LLaMa code, both Triton and CUDA branches
  * Works with AutoGPTQ
  * Works with text-generation-webui one-click-installers
  * Parameters: Groupsize = None. act-order
  * Command used to create the GPTQ:
    ```
    python llama.py /workspace/process/TheBloke_guanaco-65B-GGML/HF  wikitext2 --wbits 4 --true-sequential --act-order --save_safetensors /workspace/process/TheBloke_guanaco-65B-GGML/gptq/Guanaco-65B-GPTQ-4bit-128g.no-act-order.safetensors
    ```

<!-- footer start -->
## Discord

For further support, and discussions on these models and AI in general, join us at:

[TheBloke AI's Discord server](https://discord.gg/Jq4vkcDakD)

## Thanks, and how to contribute.

Thanks to the [chirper.ai](https://chirper.ai) team!

I've had a lot of people ask if they can contribute. I enjoy providing models and helping people, and would love to be able to spend even more time doing it, as well as expanding into new projects like fine tuning/training.

If you're able and willing to contribute it will be most gratefully received and will help me to keep providing more models, and to start work on new AI projects.

Donaters will get priority support on any and all AI/LLM/model questions and requests, access to a private Discord room, plus other benefits.

* Patreon: https://patreon.com/TheBlokeAI
* Ko-Fi: https://ko-fi.com/TheBlokeAI

**Patreon special mentions**: Aemon Algiz, Dmitriy Samsonov, Nathan LeClaire, Trenton Dambrowitz, Mano Prime, David Flickinger, vamX, Nikolai Manek, senxiiz, Khalefa Al-Ahmad, Illia Dulskyi, Jonathan Leane, Talal Aujan, V. Lukas, Joseph William Delisle, Pyrater, Oscar Rangel, Lone Striker, Luke Pendergrass, Eugene Pentland, Sebastain Graf, Johann-Peter Hartman.

Thank you to all my generous patrons and donaters!
<!-- footer end -->

# Original model card

# Guanaco Models Based on LLaMA

| [Paper](https://arxiv.org/abs/2305.14314) | [Code](https://github.com/artidoro/qlora) | [Demo](https://huggingface.co/spaces/uwnlp/guanaco-playground-tgi) | 

**The Guanaco models are open-source finetuned chatbots obtained through 4-bit QLoRA tuning of LLaMA base models on the OASST1 dataset. They are available in 7B, 13B, 33B, and 65B parameter sizes.**

⚠️Guanaco is a model purely intended for research purposes and could produce problematic outputs.

## Why use Guanaco?
- **Competitive with commercial chatbot systems on the Vicuna and OpenAssistant benchmarks** (ChatGPT and BARD) according to human and GPT-4 raters. We note that the relative performance on tasks not covered in these benchmarks could be very different. In addition, commercial systems evolve over time (we used outputs from the March 2023 version of the models).
- **Available open-source for research purposes**. Guanaco models allow *cheap* and *local* experimentation with high-quality chatbot systems.
- **Replicable and efficient training procedure** that can be extended to new use cases. Guanaco training scripts are available in the [QLoRA repo](https://github.com/artidoro/qlora).
- **Rigorous comparison to 16-bit methods** (both 16-bit full-finetuning and LoRA) in [our paper](https://arxiv.org/abs/2305.14314) demonstrates the effectiveness of 4-bit QLoRA finetuning. 
- **Lightweight** checkpoints which only contain adapter weights.

## License and Intended Use
Guanaco adapter weights are available under Apache 2 license. Note the use of the Guanaco adapter weights, requires access to the LLaMA model weighs. 
Guanaco is based on LLaMA and therefore should be used according to the LLaMA license. 

## Usage
Here is an example of how you would load Guanaco 7B in 4-bits:
```python
import torch
from peft import PeftModel    
from transformers import AutoModelForCausalLM, AutoTokenizer, BitsAndBytesConfig

model_name = "huggyllama/llama-7b"
adapters_name = 'timdettmers/guanaco-7b'

model = AutoModelForCausalLM.from_pretrained(
    model_name,
    load_in_4bit=True,
    torch_dtype=torch.bfloat16,
    device_map="auto",
    max_memory= {i: '24000MB' for i in range(torch.cuda.device_count())},
    quantization_config=BitsAndBytesConfig(
        load_in_4bit=True,
        bnb_4bit_compute_dtype=torch.bfloat16,
        bnb_4bit_use_double_quant=True,
        bnb_4bit_quant_type='nf4'
    ),
)
model = PeftModel.from_pretrained(model, adapters_name)
tokenizer = AutoTokenizer.from_pretrained(model_name)

```
Inference can then be performed as usual with HF models as follows:
```python
prompt = "Introduce yourself"
formatted_prompt = (
    f"A chat between a curious human and an artificial intelligence assistant."
    f"The assistant gives helpful, detailed, and polite answers to the user's questions.\n"
    f"### Human: {prompt} ### Assistant:"
)
inputs = tokenizer(formatted_prompt, return_tensors="pt").to("cuda:0")
outputs = model.generate(inputs=inputs.input_ids, max_new_tokens=20)
print(tokenizer.decode(outputs[0], skip_special_tokens=True))
```
Expected output similar to the following:
```
A chat between a curious human and an artificial intelligence assistant. The assistant gives helpful, detailed, and polite answers to the user's questions.
### Human: Introduce yourself ### Assistant: I am an artificial intelligence assistant. I am here to help you with any questions you may have.
```


## Current Inference Limitations 
Currently, 4-bit inference is slow. We recommend loading in 16 bits if inference speed is a concern. We are actively working on releasing efficient 4-bit inference kernels.

Below is how you would load the model in 16 bits:
```python
model_name = "huggyllama/llama-7b"
adapters_name = 'timdettmers/guanaco-7b'
model = AutoModelForCausalLM.from_pretrained(
    model_name,
    torch_dtype=torch.bfloat16,
    device_map="auto",
    max_memory= {i: '24000MB' for i in range(torch.cuda.device_count())},
)
model = PeftModel.from_pretrained(model, adapters_name)
tokenizer = AutoTokenizer.from_pretrained(model_name)

```


## Model Card
**Architecture**: The Guanaco models are LoRA adapters to be used on top of LLaMA models. They are added to all layers. For all model sizes, we use $r=64$.

**Base Model**: Guanaco uses LLaMA as base model with sizes 7B, 13B, 33B, 65B. LLaMA is a causal language model pretrained on a large corpus of text. See [LLaMA paper](https://arxiv.org/abs/2302.13971) for more details. Note that Guanaco can inherit biases and limitations of the base model.

**Finetuning Data**: Guanaco is finetuned on OASST1. The exact dataset is available at [timdettmers/openassistant-guanaco](https://huggingface.co/datasets/timdettmers/openassistant-guanaco).

**Languages**: The OASST1 dataset is multilingual (see [the paper](https://arxiv.org/abs/2304.07327) for details) and as such Guanaco responds to user queries in different languages. We note, however, that OASST1 is heavy in high-resource languages. In addition, human evaluation of Guanaco was only performed in English and based on qualitative analysis we observed degradation in performance in other languages. 

Next, we describe Training and Evaluation details.

### Training
Guanaco models are the result of 4-bit QLoRA supervised finetuning on the OASST1 dataset. 

All models use NormalFloat4 datatype for the base model and LoRA adapters on all linear layers with BFloat16 as computation datatype. We set LoRA $r=64$, $\alpha=16$. We also use Adam beta2 of 0.999, max grad norm of 0.3 and LoRA dropout of 0.1 for models up to 13B and 0.05 for 33B and 65B models.
For the finetuning process, we use constant learning rate schedule and paged AdamW optimizer. 

### Training hyperparameters
Size| Dataset | Batch Size | Learning Rate | Max Steps | Sequence length 
---|---|---|---|---|---
7B | OASST1      | 16 | 2e-4 | 1875 | 512
13B | OASST1     | 16 | 2e-4 | 1875 | 512
33B | OASST1     | 16 | 1e-4 | 1875 | 512
65B | OASST1     | 16 | 1e-4 | 1875 | 512

### Evaluation
We test generative language capabilities through both automated and human evaluations. This second set of evaluations relies on queries curated by humans and aims at measuring the quality of model responses. We use the Vicuna and OpenAssistant datasets with 80 and 953 prompts respectively. 

In both human and automated evaluations, for each prompt, raters compare all pairs of responses across the models considered. For human raters we randomize the order of the systems, for GPT-4 we evaluate with both orders.

  
Benchmark | Vicuna |  | Vicuna |   | OpenAssistant |   | -
-----------|----|-----|--------|---|---------------|---|---
Prompts    | 80 |     | 80     |   | 953           |   |
Judge | Human | | GPT-4 | | GPT-4 | |  
Model | Elo | Rank | Elo | Rank | Elo | Rank | **Median Rank** 
GPT-4 | 1176 | 1 | 1348 | 1 | 1294 | 1 | 1 
Guanaco-65B | 1023 | 2 | 1022 | 2 | 1008 | 3 | 2 
Guanaco-33B | 1009 | 4 | 992 | 3 | 1002 | 4 | 4 
ChatGPT-3.5 Turbo | 916 | 7 | 966 | 5 | 1015 | 2 | 5 
Vicuna-13B | 984 | 5 | 974 | 4 | 936 | 5 | 5 
Guanaco-13B | 975 | 6 | 913 | 6 | 885 | 6 | 6 
Guanaco-7B | 1010 | 3 | 879 | 8 | 860 | 7 | 7 
Bard | 909 | 8 | 902 | 7 | - | - | 8 


We also use the MMLU benchmark to measure performance on a range of language understanding tasks. This is a multiple-choice benchmark covering 57 tasks including elementary mathematics, US history, computer science, law, and more. We report 5-shot test accuracy.

 Dataset | 7B | 13B | 33B | 65B 
---|---|---|---|---
 LLaMA no tuning | 35.1 | 46.9 | 57.8 | 63.4 
 Self-Instruct | 36.4 | 33.3 | 53.0 | 56.7 
 Longform | 32.1 | 43.2 | 56.6 | 59.7 
 Chip2 | 34.5 | 41.6 | 53.6 | 59.8 
 HH-RLHF | 34.9 | 44.6 | 55.8 | 60.1 
 Unnatural Instruct | 41.9 | 48.1 | 57.3 | 61.3 
 OASST1 (Guanaco) | 36.6 | 46.4 | 57.0 | 62.2 
 Alpaca | 38.8 | 47.8 | 57.3 | 62.5 
 FLAN v2 | 44.5 | 51.4 | 59.2 | 63.9 

## Risks and Biases
The model can produce factually incorrect output, and should not be relied on to produce factually accurate information. The model was trained on various public datasets; it is possible that this model could generate lewd, biased, or otherwise offensive outputs.

However, we note that finetuning on OASST1 seems to reduce biases as measured on the CrowS dataset. We report here the performance of Guanaco-65B compared to other baseline models on the CrowS dataset.

|                      | LLaMA-65B | GPT-3 | OPT-175B | Guanaco-65B   |
|----------------------|-----------|-------|----------|---------------|
| Gender               | 70.6      | 62.6  | 65.7     | **47.5** |
| Religion             | {79.0}    | 73.3  | 68.6     | **38.7** |
| Race/Color           | 57.0      | 64.7  | 68.6     | **45.3** |
| Sexual orientation   | {81.0}    | 76.2  | 78.6     | **59.1** |
| Age                  | 70.1      | 64.4  | 67.8     | **36.3** |
| Nationality          | 64.2      | 61.6  | 62.9     | **32.4** |
| Disability           | 66.7      | 76.7  | 76.7     | **33.9** |
| Physical appearance  | 77.8      | 74.6  | 76.2     | **43.1** |
| Socioeconomic status | 71.5      | 73.8  | 76.2     | **55.3** |
| Average              | 66.6      | 67.2  | 69.5     | **43.5** |

## Citation

```bibtex
@article{dettmers2023qlora,
  title={QLoRA: Efficient Finetuning of Quantized LLMs},
  author={Dettmers, Tim and Pagnoni, Artidoro and Holtzman, Ari and Zettlemoyer, Luke},
  journal={arXiv preprint arXiv:2305.14314},
  year={2023}
}
```
