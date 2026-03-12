from setuptools import setup, find_packages

setup(
    name="micelio",
    version="0.1.0",
    packages=find_packages(),
    python_requires=">=3.9",
    install_requires=[
        "cryptography>=41.0",
        "python-ulid>=2.0",
    ],
    extras_require={
        "discovery": ["zeroconf>=0.80"],
        "dev": ["pytest>=7.0"],
    },
)
