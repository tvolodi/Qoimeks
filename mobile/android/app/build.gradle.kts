import java.util.Properties

plugins {
    id("com.android.application")
    id("kotlin-android")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

// android/key.properties names the release key and its password; it is never committed.
val keyProperties = rootProject.file("key.properties").takeIf { it.exists() }?.let { f ->
    Properties().apply { f.inputStream().use { load(it) } }
}

android {
    namespace = "kz.aidala.qoimeks"
    compileSdk = flutter.compileSdkVersion
    ndkVersion = flutter.ndkVersion

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = JavaVersion.VERSION_17.toString()
    }

    defaultConfig {
        // TODO: Specify your own unique Application ID (https://developer.android.com/studio/build/application-id.html).
        applicationId = "kz.aidala.qoimeks"
        // You can update the following values to match your application needs.
        // For more information, see: https://flutter.dev/to/review-gradle-config.
        minSdk = flutter.minSdkVersion
        targetSdk = flutter.targetSdkVersion
        versionCode = flutter.versionCode
        versionName = flutter.versionName
    }

    signingConfigs {
        if (keyProperties != null) {
            create("release") {
                storeFile = file(keyProperties.getProperty("storeFile"))
                storePassword = keyProperties.getProperty("storePassword")
                keyAlias = keyProperties.getProperty("keyAlias")
                keyPassword = keyProperties.getProperty("keyPassword")
            }
        }
    }

    buildTypes {
        release {
            // Without android/key.properties the build is signed with the debug key,
            // so it still runs on a machine that does not hold the release key. Such a
            // build cannot be installed over one signed with the release key.
            signingConfig = signingConfigs.getByName(if (keyProperties != null) "release" else "debug")
        }
    }
}

flutter {
    source = "../.."
}
