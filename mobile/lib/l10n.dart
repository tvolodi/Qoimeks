import 'api.dart';

const kinds = ['receipt', 'issue', 'transfer', 'adjustment'];

const _ru = {
  'kind_receipt': 'Приход',
  'kind_issue': 'Расход',
  'kind_transfer': 'Перемещение',
  'kind_adjustment': 'Инвентаризация',
  'sku': 'Товар',
  'location': 'Место',
  'from': 'Откуда',
  'to': 'Куда',
  'qty': 'Количество',
  'qty_counted': 'Фактическое количество',
  'post': 'Провести',
  'search': 'Поиск',
  'nothing_found': 'Ничего не найдено',
  'balance': 'остаток',
  'also_at': 'Есть в:',
  'will_be_negative': 'Остаток станет отрицательным',
  'heard': 'Распознано',
  'which_sku': 'Какой товар?',
  'posted': 'Проведено',
  'history': 'История',
  'settings': 'Настройки',
  'server': 'Адрес сервера',
  'operator': 'Оператор (ваше имя)',
  'language': 'Язык',
  'save': 'Сохранить',
  'required': 'Обязательное поле',
  'reversal': 'Сторно',
  'reverse_q': 'Отменить эту операцию?',
  'reverse': 'Отменить операцию',
  'cancel': 'Нет',
  'reversed': 'Отменено',
  'empty_history': 'Операций пока нет',
  'speak': 'Сказать',
  'listening': 'Говорите… Нажмите ещё раз, чтобы закончить',
  'recognizing': 'Распознаю…',
  'retry': 'Повторить',
  'loading_failed': 'Не удалось загрузить справочник',
  'err_network': 'Нет связи с сервером',
  'err_voice_off': 'Голосовой ввод не настроен на сервере',
  'err_not_understood': 'Не удалось распознать. Повторите или введите вручную',
  'err_silence': 'Ничего не слышно. Повторите',
  'err_mic': 'Нет доступа к микрофону',
  'err_already_posted': 'Эта операция уже была проведена. Проверьте историю',
  'err_generic': 'Ошибка',
};

const _kk = {
  'kind_receipt': 'Кіріс',
  'kind_issue': 'Шығыс',
  'kind_transfer': 'Ауыстыру',
  'kind_adjustment': 'Түгендеу',
  'sku': 'Тауар',
  'location': 'Орын',
  'from': 'Қайдан',
  'to': 'Қайда',
  'qty': 'Саны',
  'qty_counted': 'Нақты саны',
  'post': 'Өткізу',
  'search': 'Іздеу',
  'nothing_found': 'Ештеңе табылмады',
  'balance': 'қалдық',
  'also_at': 'Басқа орындарда:',
  'will_be_negative': 'Қалдық теріс болады',
  'heard': 'Танылды',
  'which_sku': 'Қай тауар?',
  'posted': 'Өткізілді',
  'history': 'Тарих',
  'settings': 'Баптаулар',
  'server': 'Сервер мекенжайы',
  'operator': 'Оператор (атыңыз)',
  'language': 'Тіл',
  'save': 'Сақтау',
  'required': 'Міндетті өріс',
  'reversal': 'Сторно',
  'reverse_q': 'Бұл операцияның күшін жою керек пе?',
  'reverse': 'Операцияның күшін жою',
  'cancel': 'Жоқ',
  'reversed': 'Күші жойылды',
  'empty_history': 'Әзірге операциялар жоқ',
  'speak': 'Айту',
  'listening': 'Сөйлеңіз… Аяқтау үшін қайта басыңыз',
  'recognizing': 'Танып жатырмын…',
  'retry': 'Қайталау',
  'loading_failed': 'Анықтамалықты жүктеу мүмкін болмады',
  'err_network': 'Сервермен байланыс жоқ',
  'err_voice_off': 'Дауыспен енгізу серверде бапталмаған',
  'err_not_understood': 'Тану мүмкін болмады. Қайталаңыз немесе қолмен енгізіңіз',
  'err_silence': 'Ештеңе естілмеді. Қайталаңыз',
  'err_mic': 'Микрофонға рұқсат жоқ',
  'err_already_posted': 'Бұл операция бұрын өткізілген. Тарихты тексеріңіз',
  'err_generic': 'Қате',
};

class Strings {
  const Strings(this.lang);

  final String lang;

  String call(String key) => (lang == 'kk' ? _kk : _ru)[key] ?? _ru[key] ?? key;

  String kind(String kind) => call('kind_$kind');

  /// A message an operator can act on, for any failed server call.
  String error(Object e) {
    if (e is! ApiException) return '${call('err_generic')}: $e';
    return switch (e.status) {
      0 => call('err_network'),
      409 => call('err_already_posted'),
      422 => call('err_silence'),
      502 => call('err_not_understood'),
      503 => call('err_voice_off'),
      _ => '${call('err_generic')}: ${e.message}',
    };
  }
}

/// 15.0 -> "15", 2.5 -> "2.5", 0.125 -> "0.125".
String fmtQty(double v) {
  final s = v.toStringAsFixed(3);
  return s.replaceFirst(RegExp(r'\.?0+$'), '');
}
