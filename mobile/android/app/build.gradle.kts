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
            // A release is signed with the release key or not built at all: a build signed
            // with the debug key can never be updated by the real one.
            signingConfig = signingConfigs.findByName("release")
        }
    }
}

// Stop before any release task runs when android/key.properties is missing.
gradle.taskGraph.whenReady {
    val releaseTask = allTasks.any {
        it.project == project && (it.name.contains("Release") || it.name.contains("release")) &&
            (it.name.startsWith("assemble") || it.name.startsWith("bundle") || it.name.startsWith("package") || it.name.startsWith("validateSigning"))
    }
    if (releaseTask && keyProperties == null) {
        throw GradleException("android/key.properties is missing: a release build is never signed with the debug key")
    }
}

flutter {
    source = "../.."
}
