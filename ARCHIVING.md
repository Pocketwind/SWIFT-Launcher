# 전송 전문 아카이브

`partners.json`의 입력 파트너(`direction: "in"`)에 다음 옵션을 설정합니다.

```json
{
  "name": "MT Input",
  "direction": "in",
  "status": true,
  "type": "fin",
  "input_path": "./MESG/MT/input",
  "ack_path": "./MESG/MT/ack",
  "error_path": "./MESG/MT/error",
  "progress_path": "./MESG/MT/progress",
  "archive_enabled": true,
  "archive_path": "./MESG/MT/archive",
  "extension": ".fin"
}
```

- `archive_enabled: true`: 전송 성공 후 원본을 보관합니다. 옵션을 생략해도 기존 동작을 유지하도록 활성화됩니다.
- `archive_enabled: false`: 전송 성공 후 `progress`의 원본을 삭제하고 아카이브를 생성하지 않습니다.
- `archive_path`: 보관 경로입니다. 생략하면 `progress_path` 옆의 `archive` 폴더를 사용합니다. 다른 활성 파트너를 포함해 감시하는 `input_path` 안에는 지정할 수 없습니다.

전송 건마다 현재 시각의 폴더를 만들고, 그 안에 원본 파일명을 유지한 전문과 `receipt.json`을 저장합니다.

```text
MESG/MT/archive/
  20261027T063025.123456789Z/
    payment.fin
    receipt.json
```

폴더명은 UTC 전송 완료 처리 시각을 나노초까지 표시합니다. 같은 이름의 폴더가 이미 있으면 `-1`, `-2` 등의 접미사를 붙여 중복을 방지합니다. `receipt.json`에는 원래 파일명, 파트너, 전송 참조 번호 및 보관 파일명이 기록됩니다. 일반 FileAct는 별도 본문도 같은 폴더의 `payload.bin`으로 보관하며, 다른 companion이 공유할 수 있는 입력 본문 파일은 삭제하지 않습니다.

`receipt.json`은 전송 기록용으로 예약된 파일명입니다. FileAct 별도 본문을 보관하는 경우에는 `payload.bin`도 예약됩니다. 원본 이름이 예약된 이름과 같으면 덮어쓰지 않고 보류 처리합니다.

전송 결과가 불확실하거나, 전송 성공 후 보관·정리에 실패하면 원본을 `progress`에 유지하고 `.hold.json`에 사유를 기록합니다. 아카이브를 꺼도 이 보류 동작은 유지됩니다.

일반 오류의 원본은 `error_path/failed-<UTC 현재시각>/원본파일명`으로 보관합니다. 예: `failed-20261027T063025.123456789Z`. 같은 시각의 폴더가 이미 있으면 `-1`, `-2` 등의 접미사로 구분합니다.

같은 폴더의 `원본파일명.log`에는 해당 전문의 오류 메시지와 검증 상세만 UTF-8 텍스트로 기록합니다. 예를 들어 `message.fin`과 `message.fin.log`를 함께 보관합니다. 오류 로그를 저장하지 못하면 원본은 `progress`에 유지됩니다.

기존 아카이브 파일은 이동하지 않습니다. 새 구조는 애플리케이션을 재시작한 뒤 처리하는 전문부터 적용됩니다.
