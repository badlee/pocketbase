import 'package:pocketbase_sas/models/enums/card_type.dart';

class CardDetails {
  final String cardNumber;
  final CardType cardType;

  CardDetails(this.cardNumber, this.cardType);
}
