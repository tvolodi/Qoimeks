# Release notes 0.1.2 (version code 3)

Russian:
Новый вид приложения: более понятные цвета и крупные кнопки. Сообщения о состоянии теперь с значком и словом.

Kazakh:
Қолданбаның жаңа көрінісі: түстері анығырақ, батырмалары үлкенірек. Күй туралы хабарлар енді белгішемен және сөзбен беріледі.

Command for the owner (run in the repository root; the key `mobile/android/key.properties` must exist;
the last published code is read from `../release-out/latest.json`, or add `--last-published 2`):

```sh
scripts/release-android.sh \
  --notes-ru "Новый вид приложения: более понятные цвета и крупные кнопки. Сообщения о состоянии теперь с значком и словом." \
  --notes-kk "Қолданбаның жаңа көрінісі: түстері анығырақ, батырмалары үлкенірек. Күй туралы хабарлар енді белгішемен және сөзбен беріледі."
```

Publishing is described in [qoimeks-site.md](qoimeks-site.md).
