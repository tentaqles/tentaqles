# Data classes and how to spot them

| Class | What it is | Examples | Handling |
|---|---|---|---|
| public | published or harmless if published | docs, marketing copy | none |
| internal | business data, not personal | configs, metrics, non-personal logs | may appear in prompts |
| pii | identifies or relates to a natural person (LGPD Art. 5 I; GDPR Art. 4(1)) | name, e-mail, CPF, phone, address, IP, device id | may be read and processed locally; never committed, pushed, pasted into PR or issue text, or reaches memory except masked; retention, DSAR |
| sensitive | special categories (LGPD Art. 5 II; GDPR Art. 9) | health, biometrics, religion, sexual orientation, union, politics, race | Art. 11 / Art. 9 basis, DPIA or RIPD, encryption, strict access |
| financial | money and payment data | card numbers, bank accounts, CNPJ with billing, invoices | PCI DSS scope if cards, masking, audit |
| children | data of minors (LGPD Art. 14; GDPR Art. 8) | age, school, guardian consent | consent of a guardian, DPIA |
| secrets | credentials and keys | API keys, tokens, private keys, connection strings | never in files or prompts; `tq dotenv run`; tq rules |

Column-name signals: email, cpf, cnpj, rg, phone, telefone, celular, birth_date,
data_nascimento, address, endereco, cep, card_number, iban, ip_address,
device_id, health_*, diagnosis, religion, gender.

Synthetic fixtures: generate with Faker (`pt_BR` locale for CPF and phone
shapes) through a script under `scripts/` that writes to `tests/fixtures/`;
never copy production rows (tq/pii-fixture-dump denies it).
