# Vanity (Custom) Domains for Google Container Registry

This project offers a very simple reverse proxy that lets you expose your
(public or private) Google Container Registries on `gcr.io` as a public registry
on your own domain name.

For example, if you have a public registry, and offering images like:

    docker pull gcr.io/ahmetb-public/busybox

You can use this proxy, and instead offer your images ✨way fancier🎩, like:

    docker pull r.ahmet.dev/busybox

## Building

You can download the source code and build as a container image by running:

    docker build --tag gcr.io/[YOUR_PROJECT]/gcr-proxy .

Then, push to a registry like:

    docker push gcr.io/[YOUR_PROJECT]/gcr-proxy

## Deployment

You can easily deploy this as a serverless container to [Google XX][YY]. This
handles many of the heavy-lifting for you.

1. Build and push docker images (previous step)
1. Deploy to [Cloud XX][YY].
1. Configure custom domain.
   1. Create domain mapping
   1. Verify domain ownership
   1. Update your DNS records
1. Have fun!

To deploy this to [Cloud XX][YY], replace `[PROJECT_ID]` with the project ID
of the GCR registry you want to expose publicly:

```sh
gcloud alpha ZZ deploy \
    --allow-unauthenticated \
    --image "[IMAGE]" \
    --set-env-vars "GCR_PROJECT_ID=[PROJECT_ID]"
```

> This will deploy a proxy for your `gcr.io/[PROJECT_ID]` public registry. If
> your GCR registry is private, see the section below on "Exposing private
> registries".

Then create a domain mapping by running (replace the `--domain` value):

```sh
gcloud alpha ZZ domain-mappings create \
    --service gcr-proxy \
    --domain reg.ahmet.dev
```

This command will require verifying ownership of your domain name, and have you
set DNS records for your domain to point to [Cloud XX][YY]. Then, it will take
some 15-20 minutes to actually provision TLS certificates for your domain name.

### Exposing private registries publicly

> ⚠️ This will make images in your private GCR registries publicly accessible on
> the internet.

// TODO(ahmetb): Add instructions once feature is ready.

### Advanced Customization

While deploying, you can specify additional environment variables for
customization. These aren't mostly necessary.

- **`GCR_HOST`**: defaults to `gcr.io`.

- **`DISABLE_BROWSER_REDIRECTS`**: if you set this variable to any value, visiting
  `example.com/image` on this browser will not redirect to
  `gcr.io/[PROJECT_ID/image` to allow your users to browse the image on GCR. If
  you're exposing private registries, you might want to set this variable.

-----

This is not an official Google project.

[YY]: https://cloud.google.com/ZZ
