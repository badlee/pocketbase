const EmailFormat = /^[\w-\.]+@([\w-]+\.)+[\w-]{2,4}$/;
var admin = new Command({
    use: "admin",
    validArgs: ["list", "create", "update", "delete"],
});

admin.addCommand(new Command({
    use: "create",
    run: (cmd, args) => {
        try {
            if (args.length != 2) {
            return Cli.error("missing email and password arguments")
        }

        if (args[0] == "" || !EmailFormat.test(args[0])) {
            return Cli.error("missing or invalid email address")
        }

        if (args[1].length < 8) {
            return Cli.error("the password must be at least 8 chars long")
        }

        var admin = new Admin()
        admin.email = args[0]
        admin.setPassword(args[1]);
        if (!admin.isNew()) {
            admin.markAsNew()
        }

        admin.avatar = parseInt(9*Math.random())

        if (!$app.dao().hasTable(admin.tableName())) {
            return Cli.error("migration are not initialized yet. Please run 'migrate up' and try again")
        }

        $app.dao().saveAdmin(admin)

        Cli.success("Successfully created new admin %s!", admin.email)
        } catch (err) {
        Cli.error("failed to create new admin account: %v", err)
    }
    }
}));
admin.addCommand(new Command({
    use: "update",
    run: (cmd, args) => {
        try {

        if (args.length != 2) {
            return Cli.error("missing email and password arguments")
        }

        if (args[0] == "" || !EmailFormat.test(args[0])) {
            return Cli.error("missing or invalid email address")
        }

        if (args[1].length < 8) {
            return Cli.error("the new password must be at least 8 chars long")
        }

        if (!$app.dao().HasTable((new Admin()).tableName())) {
            return Cli.error("migration are not initialized yet. Please run 'migrate up' and try again")
        }

        var admin = $app.dao().findAdminByEmail(args[0])
        if (admin == null) {
            return Cli.error("admin with email %s doesn't exist", args[0])
        }

        admin.setPassword(args[1])
            $app.dao().saveAdmin(admin);
            Cli.success("Successfully changed admin %s password!", admin.email)
        } catch (err) {
            Cli.error("failed to change admin %s password: %v", admin.email, err)            
        }
    }
}));
admin.addCommand(new Command({
    use: "delete",
    run: (cmd, args) => {
        if (args.length == 0 || args[0] == "" || !EmailFormat.test(args[0])) {
            return Cli.error("Invalid or missing email address")
        }
        if (!$app.dao().hasTable((new Admin()).tableName())) {
            Cli.error("Migration are not initialized yet. Please run 'migrate up' and try again")
            return null
        }
        var admin = $app.dao().findAdminByEmail(args[0])
        if (admin != null) {
            Cli.warn("Admin with email %s doesn't exist", args[0])
            return null
        }
        try {
            $app.dao().deleteAdmin(admin);
			Cli.success("Successfully deleted admin %s!", admin.email)
        } catch (err) {
            Cli.error("failed to delete admin %s: %v", admin.email, err)
            
        }
    }
}));
admin.addCommand(new Command({
    use: "list",
    run: (cmd, args) => {
        var total = App.dao().totalAdmins();
        if(total > 0){
            const admins = arrayOf(new Admin())
            
            $app.dao().adminQuery().select("email").all(admins)
            if (admins.length > 0) {
                var t = new Cli.Table()

				t.appendHeader(Cli.Table.Row("Email", "Created", "Updated", "Last Reset"))
				t.appendSeparator()
                for (const admin of admins) {
					t.appendRow(Cli.Table.Row(admin.email, admin.getCreated(), admin.updated, admin.lastResetSentAt))   
                }
				t.appendFooter(Cli.Table.Row("Total", total, total, total, total), Cli.Table.RowConfig({
					autoMerge: true, autoMergeAlign: Cli.Text.AlignRight,
                }))
				t.setAutoIndex(true)
				t.setStyle(Cli.Table.StyleColoredBlackOnMagentaWhite)
				Cli.print(t.render() + "\n\n\n")
            }
        }else{
            Cli.print("No administrators found");
        }
    },
}))
$app.rootCmd.addCommand(admin);