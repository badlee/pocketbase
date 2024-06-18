import 'package:pocketbase/pocketbase.dart';
String url_base = const String.fromEnvironment("server_url");

if (url_base == null) {
  throw Exception("You must define url_base. This can be done "
                  "with the --dart-define arg to run or build");
}

class Server {
    static late final PocketBase _pb;
    static late final SharedPreferences _prefs;
    static void init() async {
        _prefs = await SharedPreferences.getInstance()
        final store = AsyncAuthStore(
            save:    (String data) async => prefs.setString('pb_auth', data),
            initial: prefs.getString('pb_auth'),
        );

        _pb = PocketBase(url_base, authStore: store);
    }
    
}
